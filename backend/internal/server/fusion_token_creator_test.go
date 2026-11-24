package server

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ikhnos/internal/accounts"
	"ikhnos/internal/kernel"
	"ikhnos/internal/store"
)

// creators reads the token list as an administrator and returns each token's creator mark by name.
func (d *dataRig) creators(t *testing.T, cookie string) map[string]any {
	t.Helper()
	r := d.do("GET", "/api/v1/fusion/tokens", nil, withCookie(cookie))
	if r.Code != 200 {
		t.Fatalf("list: %d %s", r.Code, r.Body.String())
	}
	out := map[string]any{}
	for _, tok := range r.jsonArray(t) {
		out[tok["name"].(string)] = tok["creator"]
	}
	return out
}

// mintAs makes a token as the person signed in with cookie and returns its secret and id.
func (d *dataRig) mintAs(t *testing.T, cookie, name string) (secret, id string) {
	t.Helper()
	r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": name}, withCookie(cookie))
	if r.Code != 200 {
		t.Fatalf("mint %s: %d %s", name, r.Code, r.Body.String())
	}
	j := r.json(t)
	return j["token"].(string), j["details"].(map[string]any)["id"].(string)
}

// fusionAudit is the organisation's trail rows whose action starts with prefix, oldest first.
func (d *dataRig) fusionAudit(t *testing.T, org, prefix string) []store.AuditEvent {
	t.Helper()
	rows, err := d.st.ListAudit(d.ctx, org, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.AuditEvent
	for i := len(rows) - 1; i >= 0; i-- { // ListAudit is newest first
		if strings.HasPrefix(rows[i].Action, prefix) {
			out = append(out, rows[i])
		}
	}
	return out
}

func TestTheTokenListMarksATokenWhoseMakerIsNoLongerAnAdministrator(t *testing.T) {
	d := newDataRig(t)
	_, owner := d.user(t, "olga", accounts.RoleOwner)
	demotedID, demoted := d.user(t, "dora", accounts.RoleAdmin)
	removedID, removed := d.user(t, "rémi", accounts.RoleAdmin)
	disabledID, disabled := d.user(t, "dina", accounts.RoleAdmin)
	deletedID, deleted := d.user(t, "dele", accounts.RoleAdmin)
	d.mintAs(t, d.admin, "root's")
	d.mintAs(t, owner, "olga's")
	d.mintAs(t, demoted, "dora's")
	d.mintAs(t, removed, "rémi's")
	d.mintAs(t, disabled, "dina's")
	d.mintAs(t, deleted, "dele's")
	// A token made before makers were recorded has a username and no account id.
	if err := d.st.CreateFusionToken(d.ctx, store.FusionToken{ID: kernel.NewFusionTokenID(), OrgID: "org-1", Name: "legacy",
		CreatedBy: "root", CreatedAt: *d.now, ExpiresAt: d.now.Add(time.Hour)}, kernel.HashSecret(kernel.RandHex(8))); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"root's": "admin", "olga's": "admin", "dora's": "admin", "legacy": "unknown"} {
		if got := d.creators(t, d.admin)[name]; got != want {
			t.Errorf("before anything changed, %s is marked %v, want %s", name, got, want)
		}
	}

	if r := d.do("POST", "/api/v1/members/"+demotedID+"/role", map[string]any{"role": accounts.RoleEditor}, withCookie(owner)); r.Code != 204 {
		t.Fatalf("demote: %d %s", r.Code, r.Body.String())
	}
	if r := d.do("POST", "/api/v1/members/"+removedID+"/remove", nil, withCookie(owner)); r.Code != 204 {
		t.Fatalf("remove: %d %s", r.Code, r.Body.String())
	}
	disabledAt := *d.now
	if err := d.st.SetDisabled(d.ctx, disabledID, &disabledAt); err != nil {
		t.Fatal(err)
	}
	if err := d.st.DeleteUser(d.ctx, deletedID); err != nil {
		t.Fatal(err)
	}
	// The legacy token's username still names an administrator, and it is still unknown: a name is not proof of who it was.
	want := map[string]string{"root's": "admin", "olga's": "admin", "dora's": "gone", "rémi's": "gone", "dina's": "gone",
		"dele's": "gone", "legacy": "unknown"}
	got := d.creators(t, d.admin)
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s is marked %v, want %s", name, got[name], w)
		}
	}

	// Being made an administrator again takes the mark away: it says what is true now, not what once happened.
	if r := d.do("POST", "/api/v1/members/"+demotedID+"/role", map[string]any{"role": accounts.RoleAdmin}, withCookie(owner)); r.Code != 204 {
		t.Fatalf("promote: %d %s", r.Code, r.Body.String())
	}
	if got := d.creators(t, d.admin)["dora's"]; got != "admin" {
		t.Errorf("after being made an administrator again, dora's is marked %v", got)
	}
}

func TestATokenWhoseMakerWasDemotedKeepsWorkingUntilSomeoneRevokesIt(t *testing.T) {
	d := newDataRig(t)
	_, owner := d.user(t, "olga", accounts.RoleOwner)
	secret, _ := d.mintAs(t, d.admin, "decider")
	if r := d.do("POST", "/api/v1/members/"+d.adminID+"/role", map[string]any{"role": accounts.RoleViewer}, withCookie(owner)); r.Code != 204 {
		t.Fatalf("demote: %d %s", r.Code, r.Body.String())
	}
	// Tokens belong to the organisation: demoting their maker revokes nothing.
	if r := d.get("/api/v1/fusion/status", bearer(secret)); r.Code != 200 {
		t.Fatalf("the token stopped working when its maker was demoted: %d", r.Code)
	}
	if rows := d.fusionAudit(t, "org-1", "fusion-token-revoked"); len(rows) != 0 {
		t.Fatalf("demoting revoked tokens: %+v", rows)
	}
	if got := d.creators(t, owner)["decider"]; got != "gone" {
		t.Fatalf("marked %v, want gone", got)
	}
}

func TestRevokingEveryTokenAPersonMadeEndsOnlyTheirsAndAuditsEachAsAManualRevoke(t *testing.T) {
	d := newDataRig(t)
	ownerID, owner := d.user(t, "olga", accounts.RoleOwner)
	s1, id1 := d.mintAs(t, d.admin, "first")
	s2, id2 := d.mintAs(t, d.admin, "second")
	keep, _ := d.mintAs(t, owner, "the owner's")
	// The same person's token in another organisation (written straight to the store: only the main organisation mints).
	if err := d.st.CreateFusionToken(d.ctx, store.FusionToken{ID: "fk-elsewhere", OrgID: "org-2", Name: "elsewhere", CreatedBy: "root",
		CreatedByID: d.adminID, CreatedAt: *d.now, ExpiresAt: d.now.Add(time.Hour)}, []byte("elsewhere")); err != nil {
		t.Fatal(err)
	}

	if r := d.do("POST", "/api/v1/members/"+d.adminID+"/remove", nil, withCookie(owner)); r.Code != 204 {
		t.Fatalf("remove: %d %s", r.Code, r.Body.String())
	}
	r := d.do("POST", "/api/v1/fusion/tokens/revoke", map[string]any{"createdById": d.adminID}, withCookie(owner))
	if r.Code != 200 {
		t.Fatalf("revoke: %d %s", r.Code, r.Body.String())
	}
	ids, _ := r.json(t)["revoked"].([]any)
	if len(ids) != 2 || !((ids[0] == id1 && ids[1] == id2) || (ids[0] == id2 && ids[1] == id1)) {
		t.Fatalf("revoked = %v, want %s and %s", ids, id1, id2)
	}
	for _, s := range []string{s1, s2} {
		if r := d.get("/api/v1/fusion/status", bearer(s)); r.Code != 401 {
			t.Errorf("a revoked token still works: %d", r.Code)
		}
	}
	if r := d.get("/api/v1/fusion/status", bearer(keep)); r.Code != 200 {
		t.Errorf("another person's token was revoked: %d", r.Code)
	}
	if other, _ := d.st.ListFusionTokens(d.ctx, "org-2"); len(other) != 1 {
		t.Errorf("another organisation's token was revoked: %+v", other)
	}

	// One row per token, exactly the row a manual revoke writes: who did it, the token's id and its name.
	rows := d.fusionAudit(t, "org-1", "fusion-token-revoked")
	if len(rows) != 2 {
		t.Fatalf("audit = %+v, want one row per token", rows)
	}
	named := map[string]string{}
	for _, e := range rows {
		if e.Actor != "olga" || e.TargetKind != "fusion-token" {
			t.Errorf("row %+v", e)
		}
		named[e.TargetID] = e.Detail
	}
	if named[id1] != "first" || named[id2] != "second" {
		t.Errorf("rows name %v", named)
	}
	if rows := d.fusionAudit(t, "org-2", "fusion-token"); len(rows) != 0 {
		t.Errorf("another organisation's trail was written: %+v", rows)
	}

	// Asking again finds nothing to revoke, which is not an error; nor is a person who never made a token.
	for _, who := range []string{d.adminID, ownerID + "-nobody"} {
		r := d.do("POST", "/api/v1/fusion/tokens/revoke", map[string]any{"createdById": who}, withCookie(owner))
		if r.Code != 200 || r.Body.String() != "{\"revoked\":[]}\n" {
			t.Errorf("revoking for %s: %d %s", who, r.Code, r.Body.String())
		}
	}
	if rows := d.fusionAudit(t, "org-1", "fusion-token-revoked"); len(rows) != 2 {
		t.Errorf("a revoke that found nothing was recorded: %+v", rows)
	}
}

func TestRevokingAPersonsTokensIsAllowedOnlyToWhoMayRevokeOne(t *testing.T) {
	d := newDataRig(t)
	_, editor := d.user(t, "eddie", accounts.RoleEditor)
	_, viewer := d.user(t, "vera", accounts.RoleViewer)
	secret, _ := d.mintAs(t, d.admin, "decider")
	// A token made before makers were recorded, which must never be matched by an empty id.
	if err := d.st.CreateFusionToken(d.ctx, store.FusionToken{ID: "fk-legacy", OrgID: "org-1", Name: "legacy", CreatedBy: "root",
		CreatedAt: *d.now, ExpiresAt: d.now.Add(time.Hour)}, []byte("legacy")); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"createdById": d.adminID}
	for who, cookie := range map[string]string{"an editor": editor, "a viewer": viewer} {
		if r := d.do("POST", "/api/v1/fusion/tokens/revoke", body, withCookie(cookie)); r.Code != 403 {
			t.Errorf("%s revoked tokens: %d %s", who, r.Code, r.Body.String())
		}
	}
	for name, bad := range map[string]map[string]any{
		"no id":         {},
		"a blank id":    {"createdById": "  "},
		"unknown field": {"createdById": d.adminID, "all": true},
	} {
		if r := d.do("POST", "/api/v1/fusion/tokens/revoke", bad, withCookie(d.admin)); r.Code != 400 {
			t.Errorf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	// An administrator of another organisation cannot reach the main organisation's tokens through their own.
	bob, bobOrg := d.register(t, "bob", "Bob Works")
	if r := d.do("POST", org(bobOrg, "fusion/tokens/revoke"), body, withCookie(bob)); r.Code != 403 {
		t.Errorf("another organisation's owner revoked tokens: %d %s", r.Code, r.Body.String())
	}
	if r := d.do("POST", org("org-1", "fusion/tokens/revoke"), body, withCookie(bob)); r.Code != 403 && r.Code != 404 {
		t.Errorf("a non-member revoked tokens: %d %s", r.Code, r.Body.String())
	}
	if r := d.get("/api/v1/fusion/status", bearer(secret)); r.Code != 200 {
		t.Fatalf("a refused request revoked the token: %d", r.Code)
	}
	if toks, _ := d.st.ListFusionTokens(d.ctx, "org-1"); len(toks) != 2 {
		t.Fatalf("tokens after the refused requests: %+v", toks)
	}
	if rows := d.fusionAudit(t, "org-1", "fusion-token-revoked"); len(rows) != 0 {
		t.Fatalf("a refused request was recorded: %+v", rows)
	}
}

func TestRevokingAPersonsTokensFailsClosedWhenTheAuditRowCannotBeWritten(t *testing.T) {
	d := newDataRig(t)
	fs := &failAudit{Store: d.st}
	d.base.Store = fs // organisations loaded from now on use it, as in TestPrivilegedActionsFailClosedWhenTheAuditRowCannotBeWritten
	secret, _ := d.mintAs(t, d.admin, "decider")
	fs.fail.Store(true)
	r := d.do("POST", "/api/v1/fusion/tokens/revoke", map[string]any{"createdById": d.adminID}, withCookie(d.admin))
	if r.Code != 500 || !strings.Contains(r.Body.String(), "audit trail could not be written") {
		t.Fatalf("revoke with a broken trail: %d %s", r.Code, r.Body.String())
	}
	fs.fail.Store(false)
	if r := d.get("/api/v1/fusion/status", bearer(secret)); r.Code != 200 {
		t.Fatalf("the token was revoked without its audit row: %d", r.Code)
	}
}

func TestMintingPurgesOnlyThisOrganisationsLapsedTokensAndRecordsThePurgeOnce(t *testing.T) {
	d := newDataRig(t)
	lapsed := func(id, org string) store.FusionToken {
		return store.FusionToken{ID: id, OrgID: org, Name: id, CreatedAt: d.now.Add(-48 * time.Hour), ExpiresAt: d.now.Add(-time.Hour)}
	}
	for _, tok := range []store.FusionToken{lapsed("fk-a", "org-1"), lapsed("fk-b", "org-1"), lapsed("fk-other", "org-2")} {
		if err := d.st.CreateFusionToken(d.ctx, tok, []byte(tok.ID)); err != nil {
			t.Fatal(err)
		}
	}
	d.mintAs(t, d.admin, "fresh")
	if toks, _ := d.st.ListFusionTokens(d.ctx, "org-1"); len(toks) != 1 || toks[0].Name != "fresh" {
		t.Fatalf("org-1 after minting: %+v", toks)
	}
	if toks, _ := d.st.ListFusionTokens(d.ctx, "org-2"); len(toks) != 1 || toks[0].ID != "fk-other" {
		t.Fatalf("minting in org-1 purged another organisation's tokens: %+v", toks)
	}
	rows := d.fusionAudit(t, "org-1", "fusion-tokens-purged")
	if len(rows) != 1 || rows[0].Actor != accounts.SystemActor.String() || rows[0].Detail != "2 expired tokens removed while root made a token" {
		t.Fatalf("purge rows = %+v, want one by the server with the count", rows)
	}
	if rows := d.fusionAudit(t, "org-2", "fusion-tokens-purged"); len(rows) != 0 {
		t.Fatalf("another organisation's trail was written: %+v", rows)
	}
	// Nothing lapsed this time: no row.
	d.mintAs(t, d.admin, "another")
	if rows := d.fusionAudit(t, "org-1", "fusion-tokens-purged"); len(rows) != 1 {
		t.Fatalf("a purge that removed nothing was recorded: %+v", rows)
	}
}

// deleteFailsAfter lets the first n token deletions through and fails every later one, as a database that gives out halfway
// through a run of revokes would.
type deleteFailsAfter struct {
	store.Store
	armed atomic.Bool
	left  atomic.Int32
}

func (f *deleteFailsAfter) DeleteFusionToken(ctx context.Context, org, id string, opts ...store.WriteOption) error {
	if f.armed.Load() && f.left.Add(-1) < 0 {
		return errors.New("disk I/O error")
	}
	return f.Store.DeleteFusionToken(ctx, org, id, opts...)
}

func TestARevokeThatStopsHalfwaySaysWhichTokensItAlreadyRevoked(t *testing.T) {
	d := newDataRig(t)
	fs := &deleteFailsAfter{Store: d.st}
	d.base.Store = fs // organisations loaded from now on use it
	var secrets []string
	for _, name := range []string{"one", "two", "three"} {
		s, _ := d.mintAs(t, d.admin, name)
		secrets = append(secrets, s)
	}
	fs.left.Store(1)
	fs.armed.Store(true)
	r := d.do("POST", "/api/v1/fusion/tokens/revoke", map[string]any{"createdById": d.adminID}, withCookie(d.admin))
	if r.Code != 500 {
		t.Fatalf("a revoke that failed halfway: %d %s", r.Code, r.Body.String())
	}
	body := r.json(t)
	ids, _ := body["revoked"].([]any)
	if len(ids) != 1 || !strings.HasPrefix(body["error"].(string), "1 of the tokens were revoked, then revoking the next one failed") {
		t.Fatalf("answer = %v, want the one revoked id and a message that says the rest were not", body)
	}
	working := 0
	for _, s := range secrets {
		if d.get("/api/v1/fusion/status", bearer(s)).Code == 200 {
			working++
		}
	}
	if working != 2 {
		t.Fatalf("%d tokens still work, want 2", working)
	}
	if rows := d.fusionAudit(t, "org-1", "fusion-token-revoked"); len(rows) != 1 || rows[0].TargetID != ids[0] {
		t.Fatalf("audit = %+v, want the one revoke", rows)
	}

	// When the first one already fails, the answer still carries the (empty) list, so a caller can always read it.
	fs.left.Store(0)
	r = d.do("POST", "/api/v1/fusion/tokens/revoke", map[string]any{"createdById": d.adminID}, withCookie(d.admin))
	if ids, ok := r.json(t)["revoked"].([]any); r.Code != 500 || !ok || len(ids) != 0 {
		t.Fatalf("a revoke that failed at once: %d %s", r.Code, r.Body.String())
	}
}
