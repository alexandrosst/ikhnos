package fusionhttp

import (
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
