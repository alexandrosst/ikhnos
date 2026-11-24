package fusion

import "ikhnos/internal/fusionapi"

// CreateTokenRequest is the body of POST .../fusion/tokens: what the token may read, and for how long (ExpiresInDays 0 is the default).
type CreateTokenRequest struct {
	Name       string             `json:"name"`
	Signals    []fusionapi.Signal `json:"signals"`
	Namespaces []string           `json:"namespaces"`
	Clusters   []string           `json:"clusters"`
	// Applications are ids of ikhnos applications the token is limited to (at most 20, each an application of the organisation
	// now). It then sees only what their services send, each where it runs, within Namespaces and Clusters; leave it out for
	// a token not limited to applications.
	Applications []string `json:"applications,omitempty"`
	// Members are the ids of the applications' services the administrator was shown and approves (as /fusion/applications
	// lists them): the token is pinned to those alone, and refused if one is no longer there. Left out, it is pinned to every
	// service the applications give it now.
	Members       []string `json:"members,omitempty"`
	ExpiresInDays int      `json:"expiresInDays"`
}

// CreatedTokenDoc is the one answer that ever carries the raw secret: the token, shown once, and what is kept of it.
type CreatedTokenDoc struct {
	Details TokenDoc `json:"details"`
	Token   string   `json:"token"`
}

// RevokeTokensRequest is the body of POST .../fusion/tokens/revoke: revoke every token of the organisation that the account
// CreatedByID made (a member's id, as the members list gives it).
type RevokeTokensRequest struct {
	CreatedByID string `json:"createdById"`
}

// RevokedTokensDoc is the answer: the ids of the tokens that were revoked, an empty list when that person had made none.
type RevokedTokensDoc struct {
	Revoked []string `json:"revoked"`
}
