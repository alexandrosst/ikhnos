package fusion

import (
	"net/http"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/httpkit"
	"ikhnos/internal/kernel"
)

// The token routes: administrators of the organisation FUSION belongs to, and no other.

// orgOnly refuses a token request from any organisation but the one FUSION belongs to.
func (h *API) orgOnly(w http.ResponseWriter, r *http.Request) bool {
	if h.control() == nil {
		h.fail(w, r, h.unavailable(r))
		return false
	}
	if h.For(r).OrgID != h.Org() {
		h.fail(w, r, kernel.Errf(kernel.KindForbidden, "FUSION belongs to this server's main organisation, which is the only one that can give out access to it"))
		return false
	}
	return true
}

func (h *API) listTokens(w http.ResponseWriter, r *http.Request) {
	if !h.orgOnly(w, r) {
		return
	}
	out, err := h.For(r).TokenDocs(r.Context(), h.Applications)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, 200, out)
}

// createToken is the one response that ever carries the raw secret.
func (h *API) createToken(w http.ResponseWriter, r *http.Request) {
	if !h.orgOnly(w, r) {
		return
	}
	var req CreateTokenRequest
	if err := httpkit.Decode(r, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	// One rule, the store's own: 1 to 365 days, or 0 for the default (90). (Checked here too, so the message names the field.)
	if maxDays := int(maxFusionTokenTTL.Hours() / 24); req.ExpiresInDays < 0 || req.ExpiresInDays > maxDays {
		h.fail(w, r, kernel.Errf(kernel.KindInvalid, "expiresInDays must be between 1 and %d (0 for the default, %d)", maxDays, int(defaultFusionTokenTTL.Hours()/24)))
		return
	}
	secret, tok, err := h.For(r).CreateFusionToken(r.Context(), httpkit.Principal(r), TokenSpec{
		Name: req.Name, Signals: signalStrings(req.Signals), Namespaces: req.Namespaces, Clusters: req.Clusters,
		Applications: req.Applications, Members: req.Members, Groups: h.Applications, TTL: time.Duration(req.ExpiresInDays) * 24 * time.Hour,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Whoever just made it is an administrator here: the route asks for one.
	var known []fusionapi.AppRef
	if len(tok.Applications) > 0 && h.Applications != nil { // (they were just read to check them: they exist)
		if groups, err := h.Applications(r.Context()); err == nil {
			for _, g := range groups {
				known = append(known, g.Ref())
			}
		}
	}
	httpkit.WriteJSON(w, 200, CreatedTokenDoc{Token: secret, Details: toTokenDoc(tok, CreatorAdmin, known)})
}

func (h *API) deleteToken(w http.ResponseWriter, r *http.Request) {
	if !h.orgOnly(w, r) {
		return
	}
	if err := h.For(r).DeleteFusionToken(r.Context(), httpkit.Actor(r), r.PathValue("id")); err != nil {
		h.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, 200, httpkit.OKDoc{OK: true})
}

// revokeTokensMadeBy revokes every token one person made. It is guarded exactly as revoking one token is (an administrator of
// the organisation FUSION belongs to), since it is that, repeated; it is a call of its own, not part of removing or demoting
// a member, because the accounts' routes know nothing of FUSION and a token belongs to the organisation, not its maker.
func (h *API) revokeTokensMadeBy(w http.ResponseWriter, r *http.Request) {
	if !h.orgOnly(w, r) {
		return
	}
	var req RevokeTokensRequest
	if err := httpkit.Decode(r, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	ids, err := h.For(r).RevokeFusionTokensMadeBy(r.Context(), httpkit.Actor(r), req.CreatedByID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, 200, RevokedTokensDoc{Revoked: ids})
}
