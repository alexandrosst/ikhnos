package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// FusionToken is a read credential for FUSION's data API (see internal/fusionhttp): what another system - a
// decision engine, a dashboard, a script - presents to read the metrics, logs and traces saved in FUSION without
// being a person with a session. It is deliberately not a personal access token: that one acts as its owner with
// the owner's whole role, whereas this one can read nothing but FUSION's data, only the signal types it names, and
// only the namespaces and clusters it names. Only the SHA-256 of the secret is stored (the same rule as every other
// secret here); the secret is shown once, when the token is made.
type FusionToken struct {
	ID    string
	OrgID string
	Name  string
	// Signals limits the token to some of "metrics", "logs", "traces". Empty means all three.
	Signals []string
	// Namespaces and Clusters limit what it can see to telemetry whose k8s.namespace.name / ikhnos.cluster.id is
	// one of these. Empty means no limit on that dimension. A signal that carries no such attribute is invisible to
	// a token that limits it: the limit fails closed.
	Namespaces []string
	Clusters   []string
	// Applications limits it further to the ikhnos applications of these ids (schema 21): what it may see is resolved at
	// every request to the places those applications' members run in now, within Namespaces and Clusters (see
	// internal/fusionhttp). Empty means no such limit. Like the lists above, a stored null is unreadable and the token is
	// refused: an empty list would mean "not limited to applications".
	Applications []string
	// ApplicationMembers are the members those applications had when the token was made, as an administrator approved them
	// (schema 21): its rights are never more than these, whatever the applications become later. Empty with Applications: no
	// member at all.
	ApplicationMembers []FusionTokenMember
	CreatedBy          string
	// CreatedByID is the account id of the person who made the token. CreatedBy is the username the audit trail
	// shows, which is not enough to tell later whether that person is still an administrator: an account that was
	// deleted frees its username for someone else. Empty for a token made before the id was recorded (schema 20).
	CreatedByID string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastUsed    *time.Time
}

// FusionTokenMember is one member of an application a token limited to it was made with.
type FusionTokenMember struct {
	Application string `json:"application"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Namespace   string `json:"namespace"`
	Cluster     string `json:"cluster"`
}

// errUnreadableScope marks a stored token whose scope cannot be parsed.
var errUnreadableScope = errors.New("a FUSION token's scope is unreadable")

const fusionTokenSchema = `
CREATE TABLE IF NOT EXISTS fusion_tokens (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL,
  name TEXT NOT NULL,
  hash BLOB NOT NULL UNIQUE,
  signals TEXT NOT NULL DEFAULT '[]',
  namespaces TEXT NOT NULL DEFAULT '[]',
  clusters TEXT NOT NULL DEFAULT '[]',
  applications TEXT NOT NULL DEFAULT '[]',
  application_members TEXT NOT NULL DEFAULT '[]',
  created_by TEXT NOT NULL DEFAULT '',
  created_by_id TEXT,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  last_used INTEGER
);
CREATE INDEX IF NOT EXISTS fusion_tokens_org ON fusion_tokens(org_id);
`

const fusionTokenCols = `id, org_id, name, signals, namespaces, clusters, applications, application_members, created_by, created_by_id, created_at, expires_at, last_used`

// stringsJSON writes a scope list for jsonStrings: "no limit" is [], never null, which jsonStrings refuses.
func stringsJSON(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonStrings reads a stored scope list. An unreadable one is an error, never "no limit": a token whose scope cannot be
// read must not be honoured, since an empty list means unrestricted. A JSON null is unreadable too: stringsJSON writes
// "no limit" as [] and never as null, so a null is a column written by something else, and Go would decode it to the
// same empty list as [] and so to a token with no limit.
func jsonStrings(s string) ([]string, error) {
	var v []string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, errors.New("the list is null, not [] or a list of values")
	}
	return v, nil
}

// membersJSON writes a token's pinned members for jsonMembers: none is [], never null.
func membersJSON(v []FusionTokenMember) string {
	if v == nil {
		v = []FusionTokenMember{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonMembers reads a token's pinned members as strictly as jsonStrings reads a list: null is unreadable, and so is a member
// that does not name its application, id, name, namespace and cluster, or that has anything else in it.
func jsonMembers(s string) ([]FusionTokenMember, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	var v []FusionTokenMember
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("the members are followed by something else")
	}
	if v == nil {
		return nil, errors.New("the members are null, not [] or a list of members")
	}
	for _, m := range v {
		if m.Application == "" || m.ID == "" || m.Name == "" || m.Namespace == "" || m.Cluster == "" {
			return nil, errors.New("a member does not say its application, id, name, namespace and cluster")
		}
	}
	return v, nil
}

func scanFusionToken(r scanner) (FusionToken, error) {
	var t FusionToken
	var signals, namespaces, clusters, applications, members string
	var created, expires int64
	var used sql.NullInt64
	var creatorID sql.NullString
	err := r.Scan(&t.ID, &t.OrgID, &t.Name, &signals, &namespaces, &clusters, &applications, &members, &t.CreatedBy, &creatorID, &created, &expires, &used)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FusionToken{}, ErrNotFound
		}
		return FusionToken{}, err
	}
	t.CreatedByID = creatorID.String
	var e1, e2, e3, e4, e5 error
	t.Signals, e1 = jsonStrings(signals)
	t.Namespaces, e2 = jsonStrings(namespaces)
	t.Clusters, e3 = jsonStrings(clusters)
	t.Applications, e4 = jsonStrings(applications)
	t.ApplicationMembers, e5 = jsonMembers(members)
	if err := errors.Join(e1, e2, e3, e4, e5); err != nil {
		// Identified, but allowed nothing: no signals. The caller decides whether to refuse it (lookup) or list it so it
		// can be revoked (list).
		t.Signals, t.Namespaces, t.Clusters, t.Applications, t.ApplicationMembers = nil, nil, nil, nil, nil
		return t, fmt.Errorf("%w: token %s: %v", errUnreadableScope, t.ID, err)
	}
	t.CreatedAt, t.ExpiresAt = fromMS(created), fromMS(expires)
	if used.Valid {
		lu := fromMS(used.Int64)
		t.LastUsed = &lu
	}
	return t, nil
}

// CreateFusionToken records a freshly minted token; id and hash are the caller's to generate.
func (s *SQLite) CreateFusionToken(ctx context.Context, t FusionToken, hash []byte, opts ...WriteOption) error {
	return s.write(ctx, opts, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO fusion_tokens(id, org_id, name, hash, signals, namespaces, clusters, applications, application_members, created_by, created_by_id, created_at, expires_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.OrgID, t.Name, hash, stringsJSON(t.Signals), stringsJSON(t.Namespaces), stringsJSON(t.Clusters), stringsJSON(t.Applications),
			membersJSON(t.ApplicationMembers), t.CreatedBy,
			sql.NullString{String: t.CreatedByID, Valid: t.CreatedByID != ""}, ms(t.CreatedAt), ms(t.ExpiresAt))
		return err
	})
}

// LookupFusionToken resolves a secret's hash. ErrNotFound for an unknown one. Expiry is the caller's to check.
func (s *SQLite) LookupFusionToken(ctx context.Context, hash []byte) (FusionToken, error) {
	return scanFusionToken(s.db.QueryRowContext(ctx, `SELECT `+fusionTokenCols+` FROM fusion_tokens WHERE hash=?`, hash))
}

// ListFusionTokens lists an organisation's tokens, newest first. Never a secret or hash.
func (s *SQLite) ListFusionTokens(ctx context.Context, org string) ([]FusionToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+fusionTokenCols+` FROM fusion_tokens WHERE org_id=? ORDER BY created_at DESC, id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FusionToken
	for rows.Next() {
		t, err := scanFusionToken(rows)
		if err != nil && !errors.Is(err, errUnreadableScope) {
			return nil, err
		}
		out = append(out, t) // an unreadable one is listed with no rights, so it can still be revoked
	}
	return out, rows.Err()
}

// TouchFusionToken records that a token was just used.
func (s *SQLite) TouchFusionToken(ctx context.Context, hash []byte, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE fusion_tokens SET last_used=? WHERE hash=?`, ms(now), hash)
	return err
}

// DeleteFusionToken removes one of an organisation's tokens; it stops working at once. ErrNotFound if id is not
// in org.
func (s *SQLite) DeleteFusionToken(ctx context.Context, org, id string, opts ...WriteOption) error {
	return s.write(ctx, opts, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM fusion_tokens WHERE org_id=? AND id=?`, org, id)
		if err != nil {
			return err
		}
		return needFound(res)
	})
}

// PurgeFusionTokens deletes the tokens of org that expired before olderThan, so a lapsed token does not sit in the list for
// ever, and returns how many it deleted. It used to purge every organisation's tokens with no record of it: an
// administrator minting a token in one organisation then silently removed another's. When it deletes any, the audit row
// record makes of the count is written in the same transaction (both or neither, as WithAudit does); when there is nothing
// to delete, nothing is written, so a purge that changed nothing leaves no row in the trail.
func (s *SQLite) PurgeFusionTokens(ctx context.Context, org string, olderThan time.Time,
	record func(n int) AuditEvent) (int, error) {
	var n int64
	err := s.write(ctx, nil, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM fusion_tokens WHERE org_id=? AND expires_at<?`, org, ms(olderThan))
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		if n == 0 {
			return errNoChange
		}
		if err := appendAudit(ctx, tx, record(int(n))); err != nil {
			return fmt.Errorf("%w: %v", ErrAuditWrite, err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
