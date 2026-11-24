package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFusionTokenRoundTripLookupTouchAndDelete(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tok := FusionToken{ID: "fk-1", OrgID: "org-1", Name: "decider", Signals: []string{"traces", "logs"}, Namespaces: []string{"shop", "pay"},
		CreatedBy: "alex", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if err := st.CreateFusionToken(ctx, tok, []byte("hash-1")); err != nil {
		t.Fatal(err)
	}
	got, err := st.LookupFusionToken(ctx, []byte("hash-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Signals, tok.Signals) || !reflect.DeepEqual(got.Namespaces, tok.Namespaces) || len(got.Clusters) != 0 || got.LastUsed != nil || !got.ExpiresAt.Equal(tok.ExpiresAt) {
		t.Fatalf("round trip = %+v", got)
	}
	if _, err := st.LookupFusionToken(ctx, []byte("nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown hash = %v", err)
	}
	if err := st.TouchFusionToken(ctx, []byte("hash-1"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.LookupFusionToken(ctx, []byte("hash-1"))
	if got.LastUsed == nil || !got.LastUsed.Equal(now.Add(time.Minute)) {
		t.Fatalf("last used = %v", got.LastUsed)
	}
	list, err := st.ListFusionTokens(ctx, "org-1")
	if err != nil || len(list) != 1 || list[0].ID != "fk-1" {
		t.Fatalf("list = %+v %v", list, err)
	}
	if other, _ := st.ListFusionTokens(ctx, "org-2"); len(other) != 0 {
		t.Fatalf("another organisation's list = %+v", other)
	}
	// Another organisation cannot delete it.
	if err := st.DeleteFusionToken(ctx, "org-2", "fk-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org delete = %v", err)
	}
	if err := st.DeleteFusionToken(ctx, "org-1", "fk-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LookupFusionToken(ctx, []byte("hash-1")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete = %v", err)
	}
}

func purgeRow(n int) AuditEvent {
	return AuditEvent{OrgID: "o", Actor: "system", Action: "fusion-tokens-purged", TargetKind: "fusion-token", Detail: fmt.Sprint(n)}
}

func TestAPurgeDropsOnlyTheLapsedTokensOfItsOwnOrganisationAndRecordsHowMany(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, tok := range []FusionToken{
		{ID: "old", OrgID: "o", Name: "old", CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour)},
		{ID: "older", OrgID: "o", Name: "older", CreatedAt: now.Add(-72 * time.Hour), ExpiresAt: now.Add(-48 * time.Hour)},
		{ID: "new", OrgID: "o", Name: "new", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "theirs", OrgID: "other", Name: "theirs", CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour)},
	} {
		if err := st.CreateFusionToken(ctx, tok, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.PurgeFusionTokens(ctx, "o", now, purgeRow)
	if err != nil || n != 2 {
		t.Fatalf("purged %d (%v), want 2", n, err)
	}
	if list, _ := st.ListFusionTokens(ctx, "o"); len(list) != 1 || list[0].ID != "new" {
		t.Fatalf("after the purge = %+v", list)
	}
	// Another organisation's lapsed token is its own to purge, when someone there mints one.
	if list, _ := st.ListFusionTokens(ctx, "other"); len(list) != 1 || list[0].ID != "theirs" {
		t.Fatalf("another organisation's tokens after the purge = %+v", list)
	}
	rows, err := st.ListAudit(ctx, "o", 10)
	if err != nil || len(rows) != 1 || rows[0].Action != "fusion-tokens-purged" || rows[0].Detail != "2" {
		t.Fatalf("audit = %+v (%v), want one purge row with the count", rows, err)
	}
	// Nothing left to purge: no row.
	if n, err := st.PurgeFusionTokens(ctx, "o", now, purgeRow); err != nil || n != 0 {
		t.Fatalf("a second purge removed %d (%v)", n, err)
	}
	if rows, _ := st.ListAudit(ctx, "o", 10); len(rows) != 1 {
		t.Fatalf("a purge that removed nothing was recorded: %+v", rows)
	}
}

func TestAPurgeWhoseAuditRowCannotBeWrittenRemovesNothing(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "old", OrgID: "o", Name: "old", CreatedAt: now.Add(-48 * time.Hour),
		ExpiresAt: now.Add(-24 * time.Hour)}, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER no_audit BEFORE INSERT ON audit BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if n, err := st.PurgeFusionTokens(ctx, "o", now, purgeRow); !errors.Is(err, ErrAuditWrite) || n != 0 {
		t.Fatalf("purge with a broken trail = %d, %v", n, err)
	}
	if list, _ := st.ListFusionTokens(ctx, "o"); len(list) != 1 {
		t.Fatalf("the tokens were purged although the audit row could not be written: %+v", list)
	}
}

func TestATokenKeepsTheAccountIdOfWhoeverMadeIt(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "n", CreatedBy: "alex", CreatedByID: "u-alex",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h1")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-2", OrgID: "o", Name: "m", CreatedBy: "system",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h2")); err != nil {
		t.Fatal(err)
	}
	got, err := st.LookupFusionToken(ctx, []byte("h1"))
	if err != nil || got.CreatedBy != "alex" || got.CreatedByID != "u-alex" {
		t.Fatalf("lookup = %+v (%v)", got, err)
	}
	// No id is stored as NULL, not as an empty string that could ever match an account.
	var null bool
	if err := st.db.QueryRow(`SELECT created_by_id IS NULL FROM fusion_tokens WHERE id='fk-2'`).Scan(&null); err != nil || !null {
		t.Fatalf("a token with no maker id stored %v (%v), want NULL", null, err)
	}
	list, _ := st.ListFusionTokens(ctx, "o")
	for _, tok := range list {
		if tok.ID == "fk-2" && tok.CreatedByID != "" {
			t.Fatalf("listed with maker id %q", tok.CreatedByID)
		}
	}
}

// A schema-19 database has tokens with no maker id. Opening it adds the column, leaves those tokens without one (a username
// is not matched up to an account, since a deleted account frees its name) and keeps them working.
func TestTokensFromSchemaNineteenGetNoMakerIdAndKeepWorking(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s.db")
	ctx := context.Background()
	st, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE fusion_tokens DROP COLUMN created_by_id`,
		`INSERT INTO fusion_tokens(id, org_id, name, hash, created_by, created_at, expires_at)
		 VALUES('fk-old','o','legacy',x'01','alex',1,9999999999999)`,
		`PRAGMA user_version = 19`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()

	up, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("opening a version 19 database: %v", err)
	}
	defer up.Close()
	var v int
	if err := up.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("user_version = %d (%v), want SchemaVersion %d", v, err, SchemaVersion)
	}
	got, err := up.LookupFusionToken(ctx, []byte{1})
	if err != nil || got.ID != "fk-old" || got.CreatedBy != "alex" || got.CreatedByID != "" {
		t.Fatalf("the old token reads back as %+v (%v)", got, err)
	}
}

func openFusionTokenStore(t *testing.T) *SQLite {
	t.Helper()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestAnUnreadableScopeIsAnErrorNotAnUnrestrictedToken(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "n", Namespaces: []string{"shop"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE fusion_tokens SET namespaces='not json' WHERE id='fk-1'`); err != nil {
		t.Fatal(err)
	}
	if tok, err := st.LookupFusionToken(ctx, []byte("h")); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("an unreadable scope was returned as %+v (%v)", tok, err)
	}
	// It is still listed, with no rights, so an administrator can revoke it.
	list, err := st.ListFusionTokens(ctx, "o")
	if err != nil || len(list) != 1 || list[0].ID != "fk-1" || len(list[0].Signals) != 0 || len(list[0].Namespaces) != 0 {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := st.DeleteFusionToken(ctx, "o", "fk-1"); err != nil {
		t.Fatal(err)
	}
}

// A list stored as JSON null decodes in Go to the same empty list as [], which means "no limit". It is not what this
// code writes, so it reads back as unreadable: a token whose namespaces column is null is refused, not given every
// namespace.
func TestAScopeStoredAsNullFailsClosed(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "limited", Signals: []string{"logs"},
		Namespaces: []string{"shop"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-2", OrgID: "o", Name: "open", Signals: []string{"logs"},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h2")); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := st.db.QueryRow(`SELECT namespaces FROM fusion_tokens WHERE id='fk-2'`).Scan(&stored); err != nil || stored != "[]" {
		t.Fatalf("no limit is written as %q (%v), want []", stored, err)
	}
	if tok, err := st.LookupFusionToken(ctx, []byte("h2")); err != nil || len(tok.Namespaces) != 0 {
		t.Fatalf("a token written with no limit does not read back: %+v %v", tok, err)
	}
	if _, err := st.db.Exec(`UPDATE fusion_tokens SET namespaces='null' WHERE id='fk-1'`); err != nil {
		t.Fatal(err)
	}
	if tok, err := st.LookupFusionToken(ctx, []byte("h")); !errors.Is(err, errUnreadableScope) {
		t.Fatalf("a null scope was read as %+v (%v), not refused", tok, err)
	}
	list, err := st.ListFusionTokens(ctx, "o")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v %v", list, err)
	}
	for _, tok := range list {
		if tok.ID == "fk-1" && len(tok.Signals) != 0 {
			t.Fatalf("a token with a null scope is listed with rights: %+v", tok)
		}
	}
}

// A token limited to applications keeps their ids; one that is not keeps [] (no such limit), never null.
func TestATokenKeepsTheApplicationsItIsLimitedTo(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "shop", Applications: []string{"app-1", "app-2"},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h1")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-2", OrgID: "o", Name: "open", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h2")); err != nil {
		t.Fatal(err)
	}
	got, err := st.LookupFusionToken(ctx, []byte("h1"))
	if err != nil || !reflect.DeepEqual(got.Applications, []string{"app-1", "app-2"}) {
		t.Fatalf("lookup = %+v (%v)", got, err)
	}
	var stored string
	if err := st.db.QueryRow(`SELECT applications FROM fusion_tokens WHERE id='fk-2'`).Scan(&stored); err != nil || stored != "[]" {
		t.Fatalf("no application limit is written as %q (%v), want []", stored, err)
	}
	if got, err := st.LookupFusionToken(ctx, []byte("h2")); err != nil || len(got.Applications) != 0 || got.ApplicationMembers == nil || len(got.ApplicationMembers) != 0 {
		t.Fatalf("an unlimited token reads back as %+v (%v)", got, err)
	}
	if err := st.db.QueryRow(`SELECT application_members FROM fusion_tokens WHERE id='fk-2'`).Scan(&stored); err != nil || stored != "[]" {
		t.Fatalf("no pinned members are written as %q (%v), want []", stored, err)
	}
}

// The members a token limited to applications was made with come back as they were written.
func TestATokenKeepsTheMembersItWasMadeWith(t *testing.T) {
	st := openFusionTokenStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	members := []FusionTokenMember{{Application: "app-1", ID: "s-cart", Name: "cart", Kind: "Deployment", Namespace: "shop", Cluster: "cl-1"},
		{Application: "app-1", ID: "s-db", Name: "db", Namespace: "data", Cluster: "cl-2"}}
	if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "shop", Applications: []string{"app-1"}, ApplicationMembers: members,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h1")); err != nil {
		t.Fatal(err)
	}
	got, err := st.LookupFusionToken(ctx, []byte("h1"))
	if err != nil || !reflect.DeepEqual(got.ApplicationMembers, members) {
		t.Fatalf("lookup = %+v (%v)", got, err)
	}
}

// Pinned members stored as null, as something that is not a list of members, or with a member that leaves out what it is,
// are unreadable: the token is refused, and listed with no rights.
func TestPinnedMembersThatCannotBeReadFailClosed(t *testing.T) {
	for _, bad := range []string{"null", "junk", `[{"application":"a","id":"s","name":"n","namespace":"shop"}]`,
		`[{"application":"a","id":"s","name":"n","namespace":"shop","cluster":"cl-1","aliases":["x"]}]`,
		`[{"application":"a","id":"","name":"n","namespace":"shop","cluster":"cl-1"}]`, `[] []`} {
		st := openFusionTokenStore(t)
		ctx := context.Background()
		now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		if err := st.CreateFusionToken(ctx, FusionToken{ID: "fk-1", OrgID: "o", Name: "shop", Signals: []string{"logs"}, Applications: []string{"app-1"},
			ApplicationMembers: []FusionTokenMember{{Application: "app-1", ID: "s", Name: "n", Namespace: "shop", Cluster: "cl-1"}}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, []byte("h")); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`UPDATE fusion_tokens SET application_members=? WHERE id='fk-1'`, bad); err != nil {
			t.Fatal(err)
		}
		if tok, err := st.LookupFusionToken(ctx, []byte("h")); !errors.Is(err, errUnreadableScope) {
			t.Fatalf("%s: read as %+v (%v), not refused", bad, tok, err)
		}
		if list, err := st.ListFusionTokens(ctx, "o"); err != nil || len(list) != 1 || len(list[0].Signals) != 0 || list[0].ApplicationMembers != nil {
			t.Fatalf("%s: list = %+v %v", bad, list, err)
		}
	}
}

// A database a draft of version 21 wrote (with the applications but not the members, and a column of places since dropped)
// still gets every column of 21 when it is opened, and its tokens keep working.
func TestADraftVersion21DatabaseGetsEveryColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s.db")
	ctx := context.Background()
	st, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE fusion_tokens DROP COLUMN application_members`,
		`ALTER TABLE fusion_tokens ADD COLUMN application_places TEXT NOT NULL DEFAULT '[]'`,
		`INSERT INTO fusion_tokens(id, org_id, name, hash, applications, created_by, created_at, expires_at)
		 VALUES('fk-draft','o','draft',x'01','["app-1"]','alex',1,9999999999999)`,
		`PRAGMA user_version = 21`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()
	up, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("opening a draft version 21 database: %v", err)
	}
	defer up.Close()
	var v int
	if err := up.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 21 {
		t.Fatalf("user_version = %d (%v)", v, err)
	}
	got, err := up.LookupFusionToken(ctx, []byte{1})
	// (Limited to an application with no member pinned: it reads back, and sees nothing.)
	if err != nil || got.ID != "fk-draft" || !reflect.DeepEqual(got.Applications, []string{"app-1"}) || got.ApplicationMembers == nil || len(got.ApplicationMembers) != 0 {
		t.Fatalf("the draft token reads back as %+v (%v)", got, err)
	}
	if err := up.CreateFusionToken(ctx, FusionToken{ID: "fk-new", OrgID: "o", Name: "new", ApplicationMembers: []FusionTokenMember{{Application: "a", ID: "s", Name: "n", Namespace: "x", Cluster: "c"}},
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, []byte{2}); err != nil {
		t.Fatalf("a new token on the upgraded draft: %v", err)
	}
}

// A schema-20 database has no applications column. Opening it adds one, gives every existing token [] (no application
// limit, which is what they had) and keeps them working.
func TestTokensFromSchemaTwentyAreLimitedToNoApplicationAndKeepWorking(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s.db")
	ctx := context.Background()
	st, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE fusion_tokens DROP COLUMN applications`,
		`ALTER TABLE fusion_tokens DROP COLUMN application_members`,
		`INSERT INTO fusion_tokens(id, org_id, name, hash, namespaces, created_by, created_by_id, created_at, expires_at)
		 VALUES('fk-old','o','legacy',x'01','["shop"]','alex','u-alex',1,9999999999999)`,
		`PRAGMA user_version = 20`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()

	up, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("opening a version 20 database: %v", err)
	}
	defer up.Close()
	var v int
	if err := up.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != SchemaVersion || SchemaVersion != 21 {
		t.Fatalf("user_version = %d (%v), want SchemaVersion 21", v, err)
	}
	got, err := up.LookupFusionToken(ctx, []byte{1})
	if err != nil || got.ID != "fk-old" || len(got.Applications) != 0 || got.Applications == nil || !reflect.DeepEqual(got.Namespaces, []string{"shop"}) {
		t.Fatalf("the old token reads back as %+v (%v)", got, err)
	}
	var stored, members string
	if err := up.db.QueryRow(`SELECT applications, application_members FROM fusion_tokens WHERE id='fk-old'`).Scan(&stored, &members); err != nil || stored != "[]" || members != "[]" {
		t.Fatalf("the migrated token's applications are %q and members %q (%v), want []", stored, members, err)
	}
	// The column cannot hold SQL NULL either: what it holds is a list, or the token is unreadable.
	if _, err := up.db.Exec(`UPDATE fusion_tokens SET applications=NULL WHERE id='fk-old'`); err == nil {
		t.Fatal("the applications column took a NULL")
	}
}
