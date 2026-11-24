package fusion

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"ikhnos/internal/accounts"
	"ikhnos/internal/fusionapi"
	"ikhnos/internal/kernel"
	"ikhnos/internal/store"
)

// FUSION access tokens: read-only credentials for the shared data API, minted by an administrator of the server's main organisation,
// shown once, limited to the signal types, namespaces and clusters they name, and expiring.

const (
	// MaxTokens is how many tokens an organisation may hold at once.
	MaxTokens             = 50
	maxFusionTokenName    = 64
	defaultFusionTokenTTL = 90 * 24 * time.Hour
	minFusionTokenTTL     = time.Hour
	maxFusionTokenTTL     = 365 * 24 * time.Hour
	maxScopeEntries       = 50
)

var (
	namespaceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)
	clusterRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// TokenSpec is what a new FUSION access token is allowed.
type TokenSpec struct {
	Name       string
	Signals    []string // empty = all three
	Namespaces []string // empty = every namespace
	Clusters   []string // empty = every cluster
	TTL        time.Duration
}

func cleanScopeList(what string, in []string, re *regexp.Regexp) ([]string, error) {
	var out []string
	// (An empty list means "no limit", so one that was given but cleans down to nothing is refused below.)

	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || slices.Contains(out, v) {
			continue
		}
		if !re.MatchString(v) {
			return nil, kernel.Errf(kernel.KindInvalid, "%q is not a valid %s", kernel.Printable(v, 40), what)
		}
		out = append(out, v)
	}
	if len(in) > 0 && len(out) == 0 {
		return nil, kernel.Errf(kernel.KindInvalid, "name at least one %s, or leave the list out to allow every one", what)
	}
	if len(out) > maxScopeEntries {
		return nil, kernel.Errf(kernel.KindInvalid, "a token can name at most %d %ss", maxScopeEntries, what)
	}
	return out, nil
}

// CreateFusionToken mints a FUSION access token for the person p. The secret is returned once and never stored. The token
// records p's account id as well as their username, so that the list can tell later whether its maker is still an
// administrator here (see FusionTokenCreator).
func (c *Service) CreateFusionToken(ctx context.Context, p accounts.Principal,
	spec TokenSpec) (string, store.FusionToken, error) {
	actor := p.User.Username
	name := strings.TrimSpace(spec.Name)
	if name == "" || len(name) > maxFusionTokenName || kernel.Printable(name, maxFusionTokenName+1) != name {
		return "", store.FusionToken{}, kernel.Errf(kernel.KindInvalid, "give the token a name of up to %d characters", maxFusionTokenName)
	}
	signals, err := fusionapi.NormalizeSignals(spec.Signals)
	if err != nil {
		return "", store.FusionToken{}, kernel.Errf(kernel.KindInvalid, "%v", err)
	}
	namespaces, err := cleanScopeList("namespace", spec.Namespaces, namespaceRe)
	if err != nil {
		return "", store.FusionToken{}, err
	}
	clusters, err := cleanScopeList("cluster id", spec.Clusters, clusterRe)
	if err != nil {
		return "", store.FusionToken{}, err
	}
	ttl := spec.TTL
	if ttl == 0 {
		ttl = defaultFusionTokenTTL
	}
	if ttl < minFusionTokenTTL || ttl > maxFusionTokenTTL {
		return "", store.FusionToken{}, kernel.Errf(kernel.KindInvalid, "a token lasts between an hour and %d days", int(maxFusionTokenTTL.Hours()/24))
	}
	now := c.Now()
	// A lapsed token is of no use to anyone and should not count against the cap. The purge is the server's own
	// housekeeping, so it is recorded under the server's name. A purge whose audit row cannot be written is not made, and
	// the token is still minted if the cap allows: the lapsed tokens it would have removed cannot be used anyway.
	if _, err := c.Store.PurgeFusionTokens(ctx, c.OrgID, now, c.purgeRow(actor)); err != nil {
		c.Log.Warn("could not remove the expired FUSION tokens", "err", err)
	}
	existing, err := c.Store.ListFusionTokens(ctx, c.OrgID)
	if err != nil {
		return "", store.FusionToken{}, err
	}
	if len(existing) >= MaxTokens {
		return "", store.FusionToken{}, kernel.Errf(kernel.KindConflict, "there are already %d tokens; revoke one you no longer use first", MaxTokens)
	}
	secret, err := kernel.NewFusionTokenSecret()
	if err != nil {
		return "", store.FusionToken{}, kernel.Errf(kernel.KindInternal, "could not generate a token")
	}
	tok := store.FusionToken{ID: kernel.NewFusionTokenID(), OrgID: c.OrgID, Name: name, Signals: signals, Namespaces: namespaces, Clusters: clusters,
		CreatedBy: actor, CreatedByID: p.User.ID, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	detail := fmt.Sprintf("%s: reads %s; namespaces %s; clusters %s; expires %s", name, strings.Join(signals, ", "), scopeWords(namespaces), scopeWords(clusters), tok.ExpiresAt.Format("2006-01-02"))
	if err := c.AuditedWrite(c.Store.CreateFusionToken(ctx, tok, kernel.HashSecret(secret),
		store.WithAudit(c.AuditRow(c.OrgID, actor, "fusion-token-created", "fusion-token", tok.ID, detail))), "fusion-token-created"); err != nil {
		return "", store.FusionToken{}, err
	}
	return secret, tok, nil
}

// purgeRow makes the audit row of a purge that removed n expired tokens: one row for the purge, not one for each token, since
// nobody chose to end them; they had ended already. The actor is the server, whose housekeeping it is, and the detail names
// the person whose new token set it off, so the row can be placed next to that person's fusion-token-created row.
func (c *Service) purgeRow(trigger string) func(n int) store.AuditEvent {
	return func(n int) store.AuditEvent {
		what := "token"
		if n != 1 {
			what = "tokens"
		}
		return c.AuditRow(c.OrgID, accounts.SystemActor.String(), "fusion-tokens-purged", "fusion-token", "",
			fmt.Sprintf("%d expired %s removed while %s made a token", n, what, trigger))
	}
}

func scopeWords(v []string) string {
	if len(v) == 0 {
		return "all"
	}
	return strings.Join(v, ", ")
}

// ListFusionTokens lists this organisation's tokens, never a secret.
func (c *Service) ListFusionTokens(ctx context.Context) ([]store.FusionToken, error) {
	return c.Store.ListFusionTokens(ctx, c.OrgID)
}

// TokenDocs lists this organisation's tokens as the API gives them, each with what is known of its maker's standing here.
// The members are read once for the whole list, not once per token, and not at all when no token records its maker.
func (c *Service) TokenDocs(ctx context.Context) ([]TokenDoc, error) {
	toks, err := c.Store.ListFusionTokens(ctx, c.OrgID)
	if err != nil {
		return nil, err
	}
	var admins map[string]bool
	for _, t := range toks {
		if t.CreatedByID != "" {
			if admins, err = c.administrators(ctx); err != nil {
				return nil, err
			}
			break
		}
	}
	out := make([]TokenDoc, len(toks))
	for i, t := range toks {
		out[i] = toTokenDoc(t, creatorOf(t, admins))
	}
	return out, nil
}

// administrators are the account ids of the people who may administer this organisation right now: an administrator or an
// owner whose account is not disabled. A deleted account has no membership left, so it is simply not here.
func (c *Service) administrators(ctx context.Context) (map[string]bool, error) {
	ms, err := c.Store.ListMembers(ctx, c.OrgID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range ms {
		if m.DisabledAt == nil && accounts.Allows(accounts.Standing(m.Role), accounts.ActAdminister) {
			out[m.ID] = true
		}
	}
	return out, nil
}

func creatorOf(t store.FusionToken, admins map[string]bool) FusionTokenCreator {
	switch {
	case t.CreatedByID == "":
		return CreatorUnknown
	case admins[t.CreatedByID]:
		return CreatorAdmin
	}
	return CreatorGone
}

// DeleteFusionToken revokes a token; it stops working at once.
func (c *Service) DeleteFusionToken(ctx context.Context, actor, id string) error {
	toks, err := c.Store.ListFusionTokens(ctx, c.OrgID)
	if err != nil {
		return err
	}
	for _, t := range toks {
		if t.ID == id {
			return c.revoke(ctx, actor, t)
		}
	}
	return kernel.Errf(kernel.KindNotFound, "no such token")
}

// RevokeFusionTokensMadeBy revokes every token of this organisation that the account userID made, and returns the ids of
// those it revoked (none is not an error). It is what the members page offers when an administrator is removed or
// demoted: tokens belong to the organisation and outlive their maker, so ending them too is a separate, explicit choice.
// Each token is revoked exactly as DeleteFusionToken revokes one, with its own audit row, so the trail reads the same as if
// they had been revoked one by one. A token made before makers were recorded names no one and is never matched.
func (c *Service) RevokeFusionTokensMadeBy(ctx context.Context, actor, userID string) ([]string, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, kernel.Errf(kernel.KindInvalid, "name the account whose tokens are to be revoked (createdById)")
	}
	toks, err := c.Store.ListFusionTokens(ctx, c.OrgID)
	if err != nil {
		return nil, err
	}
	revoked := []string{}
	for _, t := range toks {
		if t.CreatedByID != userID {
			continue
		}
		err := c.revoke(ctx, actor, t)
		if errors.Is(err, store.ErrNotFound) {
			continue // revoked by someone else since the list was read: it is gone either way
		}
		if err != nil {
			return revoked, c.partlyRevoked(revoked, err)
		}
		revoked = append(revoked, t.ID)
	}
	return revoked, nil
}

// partlyRevoked is the error of a revoke that stopped at one token: it says so, and carries the ids of the tokens already
// revoked (the field "revoked" of the error's answer), because those are gone whatever happened next and the caller must not
// be left to think that nothing changed. The cause keeps its kind; a cause that is not the kernel's is logged and reported
// as an internal error, as httpkit.Fail would report it.
func (c *Service) partlyRevoked(revoked []string, cause error) error {
	kind, msg := kernel.KindInternal, "internal error"
	var ke *kernel.Error
	if errors.As(cause, &ke) {
		kind, msg = ke.Kind, ke.Msg
	} else {
		c.Log.Error("revoking a person's FUSION tokens failed", "err", cause)
	}
	if len(revoked) > 0 {
		msg = fmt.Sprintf("%d of the tokens were revoked, then revoking the next one failed: %s", len(revoked), msg)
	}
	return &kernel.Error{Kind: kind, Msg: msg, Data: map[string]any{"revoked": revoked}}
}

// revoke ends one token of this organisation with its audit row, in one transaction.
func (c *Service) revoke(ctx context.Context, actor string, t store.FusionToken) error {
	return c.AuditedWrite(c.Store.DeleteFusionToken(ctx, c.OrgID, t.ID,
		store.WithAudit(c.AuditRow(c.OrgID, actor, "fusion-token-revoked", "fusion-token", t.ID, t.Name))), "fusion-token-revoked")
}

// AuthenticateFusionToken resolves a presented secret. A malformed, unknown or expired one is the same refusal.
func (c *Service) AuthenticateFusionToken(ctx context.Context, secret string) (store.FusionToken, error) {
	deny := kernel.Errf(kernel.KindUnauthenticated, "sign in required")
	if !kernel.LooksLikeFusionToken(secret) {
		return store.FusionToken{}, deny
	}
	h := kernel.HashSecret(secret)
	tok, err := c.Store.LookupFusionToken(ctx, h)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && tok.ID == "" { // (a token found with a scope that cannot be read has its id, and is refused)
			// The token may be good: the store could not say. A failed attempt would count against the caller's address.
			return store.FusionToken{}, kernel.Errf(kernel.KindUnavailable, "the server cannot check the token right now; try again")
		}
		return store.FusionToken{}, deny
	}
	now := c.Now()
	if !now.Before(tok.ExpiresAt) {
		return store.FusionToken{}, deny
	}
	if tok.LastUsed == nil || now.Sub(*tok.LastUsed) > accounts.TouchEvery {
		_ = c.Store.TouchFusionToken(ctx, h, now)
	}
	return tok, nil
}

// FusionTokenCreator is what is known of the standing of the person who made a token, in the organisation the token belongs to,
// at the moment the list is read. It is a mark for the people who look after the tokens, and changes nothing about the
// token itself: a token belongs to the organisation and keeps working whoever made it.
type FusionTokenCreator string

const (
	// CreatorAdmin: the maker is an administrator or an owner of the organisation, with an account that is not disabled.
	CreatorAdmin FusionTokenCreator = "admin"
	// CreatorGone: the maker is no longer an administrator here. They left or were removed, were given a role below
	// administrator, or their account was disabled or deleted.
	CreatorGone FusionTokenCreator = "gone"
	// CreatorUnknown: the token was made before the maker's account id was recorded, and a username alone cannot say
	// (a deleted account's name can be taken by someone else), so nothing is claimed either way.
	CreatorUnknown FusionTokenCreator = "unknown"
)

// WireWords is the list of FusionTokenCreator's words, as the generated types give it.
func (FusionTokenCreator) WireWords() []string {
	return []string{string(CreatorAdmin), string(CreatorGone), string(CreatorUnknown)}
}

type TokenDoc struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Signals    []fusionapi.Signal `json:"signals"`
	Namespaces []string           `json:"namespaces"`
	Clusters   []string           `json:"clusters"`
	// CreatedBy is the maker's username, as the audit trail shows it. CreatedByID is their account id, absent for a token
	// made before it was recorded; it is what the members page matches a person's tokens by.
	CreatedBy   string             `json:"createdBy"`
	CreatedByID string             `json:"createdById,omitempty"`
	Creator     FusionTokenCreator `json:"creator"`
	CreatedAt   string             `json:"createdAt"`
	ExpiresAt   string             `json:"expiresAt"`
	LastUsedAt  string             `json:"lastUsedAt,omitempty"`
}

func toTokenDoc(t store.FusionToken, creator FusionTokenCreator) TokenDoc {
	return TokenDoc{ID: t.ID, Name: t.Name, Signals: fusionapi.SignalsOf(t.Signals), Namespaces: fusionapi.OrEmpty(t.Namespaces),
		Clusters: fusionapi.OrEmpty(t.Clusters), CreatedBy: t.CreatedBy, CreatedByID: t.CreatedByID, Creator: creator,
		CreatedAt: kernel.RFC(t.CreatedAt), ExpiresAt: kernel.RFC(t.ExpiresAt), LastUsedAt: kernel.RFCP(t.LastUsed)}
}

func signalStrings(v []fusionapi.Signal) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = string(s)
	}
	return out
}
