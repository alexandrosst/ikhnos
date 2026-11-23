package fusionhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"ikhnos/internal/accounts"
	"ikhnos/internal/fusion"
	"ikhnos/internal/fusionapi"
	"ikhnos/internal/httpkit"
	"ikhnos/internal/kernel"
	"ikhnos/internal/store"
)

// Who is reading and what they may read.
//
// Two kinds of caller:
//
//   - a FUSION access token ("cnf_..."): minted by an administrator of the server's main organisation, shown once, read-only,
//     limited to the signal types, namespaces and clusters it names, and it expires. The one a script, a dashboard or a decision
//     engine holds.
//   - a person signed in (a session, or a personal access token) as an administrator of that organisation: the full read, which
//     is what lets the UI explore the data without minting a token for itself.
//
// What a caller may read is a fusionapi.Scope; the queries and the answers are checked against it in internal/fusionapi. The
// stores themselves authenticate nothing and are not exposed outside the cluster: everything goes through the guard.

const (
	// ReadsPerMinute is how many reads a credential may make a minute (a bulk read of n traces is n, and so is a read that asks
	// the stores n times: see chargeReads), ReadBurst how many at once.
	ReadsPerMinute = 600
	ReadBurst      = 60
	// RequestTimeout is how long one request may take, all the reads it makes together.
	RequestTimeout = 30 * time.Second
	// WriteGrace is how long after the request budget the connection can still be written to, so the partial answer (the 504
	// items of a bulk read, the last line of a stream) reaches the caller. The http.Server's WriteTimeout is as long as the
	// budget and started earlier, so without it the budget would end after the connection could be written.
	WriteGrace = 5 * time.Second
)

// Caller is who is reading and what they may read.
type Caller struct {
	Kind      string // token | user
	ID        string
	Name      string
	Scope     fusionapi.Scope
	ExpiresAt *time.Time
}

// Handler answers one route of the data API, for a caller the guard has authenticated, with a client for the stores.
type Handler func(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller)

// Accounts is what the guard asks of the accounts to know a person.
type Accounts interface {
	Authenticate(ctx context.Context, secret string) (accounts.Principal, error)
	AuthenticateAPIToken(ctx context.Context, secret string) (accounts.Principal, error)
	Member(ctx context.Context, p accounts.Principal, org string) (accounts.Principal, error)
}

// Tokens is what the guard asks of FUSION's access tokens.
type Tokens interface {
	AuthenticateFusionToken(ctx context.Context, secret string) (store.FusionToken, error)
}

// Guard authenticates the caller of a data route, rate-limits it, bounds the request, and hands the handler a client for the
// stores and the Scope the caller may read with. It is also what the handlers fail and narrow through.
type Guard struct {
	// Control is FUSION's control, read per request: nil when the server has none, and then there is nothing to read.
	Control func() *fusion.FusionControl
	// Org is the organisation FUSION belongs to.
	Org      func() string
	Accounts Accounts
	Tokens   Tokens
	// Bearer and Session read the credential a request carries: the Authorization header, the session cookie.
	Bearer, Session func(r *http.Request) (string, bool)
	// ClientIP is the address a request came from, as the server trusts it to be.
	ClientIP func(r *http.Request) string
	// AuthFailures throttles failed authentications per address, the same limiter as the rest of the API; Reads limits how fast
	// one authenticated caller may read.
	AuthFailures, Reads *kernel.Limiter
	// Extras is what ikhnos itself knows (applications, topology, events); nil on a server with no platform behind it.
	Extras func() fusionapi.Extras
	// Links answers the latest figures of the links between clusters. It reads the links the organisation watches (its settings), which
	// are ikhnos's and not this package's; the route and its description are here.
	Links Handler
	// Now is the clock a time range is read against.
	Now func() time.Time
	Log *slog.Logger
}

// Serve authenticates the caller, rate-limits it, bounds the request, and calls h.
func (g *Guard) Serve(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc := g.Control()
		if fc == nil { // nothing to read, so nothing to authenticate for
			httpkit.WriteErr(w, http.StatusServiceUnavailable, fc.Status(r.Context()).Message)
			return
		}
		who, err := g.Caller(r)
		if err != nil {
			var e *kernel.Error
			if errors.As(err, &e) && e.Kind == kernel.KindForbidden {
				httpkit.WriteErr(w, http.StatusForbidden, e.Msg)
				return
			}
			if errors.As(err, &e) && e.Kind == kernel.KindUnavailable {
				w.Header().Set("Retry-After", "5")
				httpkit.WriteErr(w, http.StatusServiceUnavailable, e.Msg)
				return
			}
			kernel.Metrics.AuthFailures.Add(1)
			// Failed authentications are throttled per address, the same as the rest of the API.
			if !g.AuthFailures.Allow(kernel.LimitKey(g.ClientIP(r))) {
				httpkit.WriteErr(w, http.StatusTooManyRequests, "too many failed attempts, wait a minute")
				return
			}
			httpkit.WriteErr(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if !g.Allow(who) {
			tooMany(w, "too many requests, slow down")
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(RequestTimeout + WriteGrace))
		ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
		defer cancel()
		h(w, r.WithContext(ctx), fc.DataClient(), who)
	})
}

// Allow charges one read to the caller's rate limit, reporting whether it may go on. A route that does the work of several reads (a
// bulk read of n traces, an overview, the list of applications) charges the others itself (bulkAllowed, chargeReads).
func (g *Guard) Allow(who Caller) bool { return g.Reads.Allow(who.Kind + "|" + who.ID) }

// chargeReads charges the caller's rate limit for a request that makes n store reads, of which Serve has charged the
// first, and writes the 429 Serve would when the allowance is short. The charge is taken whole or not at all, as
// bulkAllowed's is. It is at most half the limiter's burst: a request that reads more than that (every application's
// three stores, when there are many) must still be one a caller can make while its other reads go on, not only with an
// allowance no read has touched for a while.
func (g *Guard) chargeReads(w http.ResponseWriter, who Caller, n int) bool {
	n = min(n, max(g.Reads.Burst()/2, 1))
	if n > 1 && !g.Reads.AllowN(who.Kind+"|"+who.ID, n-1) {
		tooMany(w, fmt.Sprintf("too many requests, slow down; this one counts as %d reads, one for each store read it makes", n))
		return false
	}
	return true
}

// tooMany is the answer to a caller whose reads per minute are used: a 429 that says when to come back.
func tooMany(w http.ResponseWriter, msg string) {
	w.Header().Set("Retry-After", "5")
	httpkit.WriteErr(w, http.StatusTooManyRequests, msg)
}

// Caller works out who is asking: a FUSION token, or a signed-in administrator of the FUSION organisation.
func (g *Guard) Caller(r *http.Request) (Caller, error) {
	ctx := r.Context()
	deny := kernel.Errf(kernel.KindUnauthenticated, "sign in required")
	var p accounts.Principal
	var err error
	// A credential given on purpose beats one the browser sends along: someone trying a token on the docs page, signed in as
	// an administrator, must be reading with the token's rights and not the administrator's.
	if secret, ok := g.Bearer(r); ok {
		if kernel.LooksLikeFusionToken(secret) {
			tok, err := g.Tokens.AuthenticateFusionToken(ctx, secret)
			var ke *kernel.Error
			if err != nil && errors.As(err, &ke) && ke.Kind == kernel.KindUnavailable {
				return Caller{}, err // the store is down: not the caller's mistake, and not a failed attempt
			}
			if err != nil || tok.OrgID != g.Org() {
				return Caller{}, deny
			}
			exp := tok.ExpiresAt
			return Caller{Kind: "token", ID: tok.ID, Name: tok.Name, ExpiresAt: &exp,
				Scope: fusionapi.Scope{Signals: tok.Signals, Namespaces: tok.Namespaces, Clusters: tok.Clusters}}, nil
		}
		p, err = g.Accounts.AuthenticateAPIToken(ctx, secret)
	} else if secret, ok := g.Session(r); ok {
		p, err = g.Accounts.Authenticate(ctx, secret)
	} else {
		return Caller{}, deny
	}
	if err != nil {
		return Caller{}, deny
	}
	if p.User.MustChange {
		return Caller{}, kernel.Errf(kernel.KindForbidden, "you must choose a new password before doing anything else")
	}
	if p, err = g.Accounts.Member(ctx, p, g.Org()); err != nil {
		var ke *kernel.Error
		if errors.As(err, &ke) && ke.Kind == kernel.KindNotFound {
			// Signed in, but not in the FUSION organisation: who they are is known, so the answer is "not allowed", not "sign in".
			return Caller{}, kernel.Errf(kernel.KindForbidden, "FUSION's data is read by administrators of the organisation that runs it, or with a FUSION access token")
		}
		return Caller{}, kernel.Errf(kernel.KindUnavailable, "the server cannot check who you are right now; try again")
	}
	if !accounts.Allows(p, accounts.ActAdminister) {
		return Caller{}, kernel.Errf(kernel.KindForbidden, "reading FUSION's data takes an administrator of the organisation, or a FUSION access token")
	}
	return Caller{Kind: "user", ID: p.User.ID, Name: p.User.Username, Scope: fusionapi.AllSignals()}, nil
}

// Err writes a failure from the query package (or the request's own context).
func (g *Guard) Err(w http.ResponseWriter, err error) {
	var fe *fusionapi.Error
	switch {
	case errors.As(err, &fe):
		httpkit.WriteErr(w, fe.Status, fe.Msg)
	case errors.Is(err, context.DeadlineExceeded):
		httpkit.WriteErr(w, http.StatusGatewayTimeout, "FUSION took too long to answer; narrow the time range or the filters")
	case errors.Is(err, context.Canceled):
		// The caller went away; nothing to tell it.
	default:
		httpkit.Fail(w, g.Log, err)
	}
}
