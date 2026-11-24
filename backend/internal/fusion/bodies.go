package fusion

import "ikhnos/internal/fusionapi"

// CreateTokenRequest is the body of POST .../fusion/tokens: what the token may read, and for how long (ExpiresInDays 0 is the default).
type CreateTokenRequest struct {
	Name          string             `json:"name"`
	Signals       []fusionapi.Signal `json:"signals"`
	Namespaces    []string           `json:"namespaces"`
	Clusters      []string           `json:"clusters"`
	ExpiresInDays int                `json:"expiresInDays"`
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
