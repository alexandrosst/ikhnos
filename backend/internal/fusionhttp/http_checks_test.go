package fusionhttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/kernel"
)

func opNamed(t *testing.T, method, path string) Op {
	t.Helper()
	for _, op := range (&Guard{}).Ops() {
		if op.Method == method && op.Path == path {
			return op
		}
	}
	t.Fatalf("no route %s %s", method, path)
	return Op{}
}

// A value given twice that is read once, and an empty one, would be silently dropped, or silently widen the read; both are told.
func TestAParameterGivenTwiceOrEmptyIsRefused(t *testing.T) {
	logs := opNamed(t, "GET", "/logs")
	for q, want := range map[string]string{
		"service=a&service=b": "given 2 times",
		"limit=5&limit=99999": "given 2 times",
		"service=":            "is empty",
	} {
		v, _ := url.ParseQuery(q)
		err := logs.CheckQuery(v)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", q, err, want)
		}
	}
	trace := opNamed(t, "GET", "/traces/{id}")
	v, _ := url.ParseQuery("include=logs&include=metrics")
	if err := trace.CheckQuery(v); err == nil || !strings.Contains(err.Error(), "separate several values with commas") {
		t.Errorf("a list given twice: %v", err)
	}
	v, _ = url.ParseQuery("fused=true&promql=a=up&promql=b=up")
	if err := trace.CheckQuery(v); err != nil {
		t.Fatalf("promql is given once per query: %v", err)
	}
}

func TestStreamWithoutFusedIsRefused(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	w := httptest.NewRecorder()
	g.Traces(w, httptest.NewRequest("GET", "/traces?stream=true", nil), &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "fused") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// A batch refused for want of room does not use up the room it found.
func TestARefusedBatchLeavesTheRoomItFound(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Reads = kernel.NewLimiter(1, 10)
	who := Caller{Kind: "token", ID: "t"}
	if w := httptest.NewRecorder(); g.bulkAllowed(w, who, 25) || w.Code != http.StatusTooManyRequests {
		t.Fatalf("25 traces with room for 10: allowed, code %d", w.Code)
	}
	if !g.bulkAllowed(httptest.NewRecorder(), who, 10) {
		t.Fatal("the refused batch used up the room of a smaller one")
	}
}

func TestLimiterAllowNIsAllOrNothing(t *testing.T) {
	l := kernel.NewLimiter(1, 5)
	if l.AllowN("k", 6) || !l.AllowN("k", 5) || l.Allow("k") {
		t.Fatal("AllowN takes all it asks for or nothing")
	}
}

// A signed-in person who is not in FUSION's organisation is known, so they are refused, not asked to sign in; a store that cannot
// check a token is unavailable, and is no failed attempt of the caller's.
func TestNotAMemberIsForbiddenAndAStoreThatIsDownIsUnavailable(t *testing.T) {
	g := newGuard(fakeAccounts{p: admin, memberr: kernel.Errf(kernel.KindNotFound, "no such organisation")}, fakeTokens{})
	if w, _ := call(g, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "s", Value: "x"}) }); w.Code != http.StatusForbidden {
		t.Fatalf("not a member: %d %s", w.Code, w.Body.String())
	}
	g = newGuard(fakeAccounts{}, fakeTokens{err: kernel.Errf(kernel.KindUnavailable, "the server cannot check the token right now; try again")})
	g.AuthFailures = kernel.NewLimiter(60, 1)
	for i := 0; i < 3; i++ {
		w, _ := call(g, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+fusionSecret) })
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d with the store down: %d %s", i, w.Code, w.Body.String())
		}
	}
}

func TestAppTopologyTakesAtLikeFromAndRefusesAZeroWindow(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	read := func(q string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/x?"+q, nil)
		r.SetPathValue("name", "Shop")
		w := httptest.NewRecorder()
		g.AppTopology(w, r, &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
		return w
	}
	if w := read("window=0"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "longer than 0") {
		t.Fatalf("window=0: %d %s", w.Code, w.Body.String())
	}
	if w := read("at=now%2B1h"); w.Code != http.StatusBadRequest {
		t.Fatalf("a malformed at: %d", w.Code)
	}
	if w := read("at=2026-10-02T00:00:00Z"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "future") {
		t.Fatalf("a future at: %d %s", w.Code, w.Body.String())
	}
	// now-3d is read (the documented example); with no applications known the answer is then about that, not about the time.
	if w := read("at=now-3d"); strings.Contains(w.Body.String(), "at:") {
		t.Fatalf("at=now-3d: %d %s", w.Code, w.Body.String())
	}
}

func TestLimiterAllowNOfNothingChargesNothing(t *testing.T) {
	l := kernel.NewLimiter(1, 2)
	if !l.AllowN("k", 0) || !l.AllowN("k", -5) || !l.AllowN("k", 2) || l.Allow("k") {
		t.Fatal("a charge of nothing, or less, is allowed and adds no tokens")
	}
}

// A search whose queries name the application without one is refused before the search, not once per hit.
func TestApplicationVariablesWithoutAnApplicationAreRefusedBeforeTheSearch(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/traces?fused=true&promql="+url.QueryEscape(`a=count(ikhnos_application_info{application_id="${application_id}"})`), nil)
	g.Traces(w, r, &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "application=") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// A read that asks the stores several times is charged each time, not once per request: a service's overview reads the
// stores five times, and the list of applications three times for each application. The first read is the one Serve
// charges.
func TestAnOverviewAndTheApplicationsAreChargedTheReadsTheyMake(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var groups []fusionapi.AppGroup
	for i := range 3 {
		groups = append(groups, fusionapi.AppGroup{ID: fmt.Sprintf("app-%d", i), Name: fmt.Sprintf("App %d", i),
			Members: []fusionapi.AppMember{{Name: fmt.Sprintf("svc%d", i), Namespace: "a", Cluster: "cl-1"}}})
	}
	setup := func() (*Guard, Caller, string) {
		g := newGuard(fakeAccounts{}, fakeTokens{})
		g.Now = func() time.Time { return now }
		g.Reads = kernel.NewLimiter(1, 20)
		g.Extras = func() fusionapi.Extras { return extrasWith{groups: groups} }
		who := Caller{Kind: "token", ID: "t", Scope: fusionapi.AllSignals()}
		return g, who, who.Kind + "|" + who.ID
	}
	served := func(g *Guard, who Caller, h Handler, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		if !g.Allow(who) { // what Serve charges
			t.Fatalf("%s: the first read was refused", path)
		}
		r := httptest.NewRequest("GET", path, nil)
		r.SetPathValue("name", "cart")
		h(w, r, &fusionapi.Client{}, who)
		return w
	}
	g, who, key := setup()
	if w := served(g, who, g.Service, "/services/cart"); w.Code == http.StatusTooManyRequests {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if g.Reads.AllowN(key, 16) || !g.Reads.AllowN(key, 15) {
		t.Fatal("a service's overview was not charged the five reads it makes")
	}

	g, who, key = setup()
	if w := served(g, who, g.Applications, "/applications"); w.Code == http.StatusTooManyRequests {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if g.Reads.AllowN(key, 12) || !g.Reads.AllowN(key, 11) {
		t.Fatal("three applications were not charged three reads each")
	}

	// Short of the allowance, the request is refused the way Serve refuses one, and takes nothing.
	g, who, key = setup()
	g.Reads.AllowN(key, 17)
	w := served(g, who, g.Service, "/services/cart")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "5 reads") {
		t.Fatalf("%d %q %s", w.Code, w.Header().Get("Retry-After"), w.Body.String())
	}
	if !g.Reads.AllowN(key, 2) {
		t.Fatal("a refused overview used up the room it found")
	}

	// A caller who may read traces alone is charged for the trace searches alone.
	g, who, key = setup()
	who.Scope.Signals = []string{fusionapi.SignalTraces}
	served(g, who, g.Service, "/services/cart")
	if g.Reads.AllowN(key, 19) || !g.Reads.AllowN(key, 18) {
		t.Fatal("a traces-only caller was charged for stores it does not read")
	}
}

// A request that would cost more than half the burst costs half of it, so a caller can make it while its other reads go
// on, not only with an allowance nothing has touched.
func TestAReadDearerThanHalfTheBurstCanBeMadeWhileOtherReadsGoOn(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Reads = kernel.NewLimiter(1, 10)
	who := Caller{Kind: "token", ID: "t"}
	if !g.Reads.AllowN(who.Kind+"|"+who.ID, 4) {
		t.Fatal("the other reads were refused")
	}
	if !g.Allow(who) || !g.chargeReads(httptest.NewRecorder(), who, 75) {
		t.Fatal("a read dearer than the burst was refused with half the allowance free")
	}
	if !g.Allow(who) || g.Allow(who) {
		t.Fatal("it was not charged half the burst")
	}
}
