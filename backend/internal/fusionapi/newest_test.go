package fusionapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTheNewestLogLineAndTraceAreAskedForOneEach(t *testing.T) {
	f := newFake(t)
	line := time.Date(2026, 10, 5, 11, 58, 0, 0, time.UTC)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"api"},"values":[["%d","hello"]]}]}}`, line.UnixNano())
	}
	start := time.Date(2026, 10, 5, 11, 59, 0, 0, time.UTC)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"traces":[{"traceID":"0123456789abcdef0123456789abcdef","rootServiceName":"api","rootTraceName":"GET /","startTimeUnixNano":"%d","durationMs":3}]}`, start.UnixNano())
	}
	c, ctx := f.client(), context.Background()

	got, ok, err := c.NewestLog(ctx, AllSignals(), NewestWindow)
	if err != nil || !ok || !got.Equal(line) {
		t.Fatalf("log: %v %v %v", got, ok, err)
	}
	q := f.last("/loki/api/v1/query_range")
	if q.Get("direction") != "backward" || q.Get("limit") != "2" || !strings.Contains(q.Get("query"), "service_name") {
		t.Errorf("asked Loki for %v", q)
	}
	got, ok, err = c.NewestTrace(ctx, AllSignals(), NewestWindow)
	if err != nil || !ok || !got.Equal(start) {
		t.Fatalf("trace: %v %v %v", got, ok, err)
	}
	if q := f.last("/api/search"); q.Get("limit") != "21" || q.Get("q") != "{ true }" {
		t.Errorf("asked Tempo for %v", q)
	}
}

// Tempo does not answer a wide search with the newest traces first, so the newest trace is looked for in short windows before long ones,
// and of the traces a window holds the newest is taken: a store that is being sent to now must not say "12 minutes ago".
func TestTheNewestTraceIsLookedForInShortWindowsFirst(t *testing.T) {
	f := newFake(t)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) // (what f.client() says it is)
	older := now.Add(-12 * time.Minute)
	recent := now.Add(-20 * time.Second)
	var mu sync.Mutex
	var asked []string
	ask := func(from int64) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, time.Unix(from, 0).UTC().Format(time.RFC3339))
	}
	count := func() ([]string, int) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...), len(asked)
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		from, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		ask(from)
		var traces []string
		for i, at := range []time.Time{older, recent, now.Add(-45 * time.Second)} {
			// (a wide window answers with the old one first, as Tempo does)
			if at.Unix() >= from {
				traces = append(traces, fmt.Sprintf(`{"traceID":"%032x","rootServiceName":"api","rootTraceName":"GET /","startTimeUnixNano":"%d","durationMs":3}`, i+1, at.UnixNano()))
			}
		}
		fmt.Fprintf(w, `{"traces":[%s]}`, strings.Join(traces, ","))
	}
	c, ctx := f.client(), context.Background()
	got, ok, err := c.NewestTrace(ctx, AllSignals(), NewestWindow)
	if err != nil || !ok || !got.Equal(recent) {
		t.Fatalf("newest trace %v %v %v, want %v", got, ok, err, recent)
	}
	if all, n := count(); n != 1 {
		t.Errorf("a recent trace was found in the first window, yet Tempo was asked %d times: %v", n, all)
	}
	// With only an old trace the wider windows are tried in turn, and the old one is the answer.
	mu.Lock()
	asked = nil
	mu.Unlock()
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		from, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		ask(from)
		if older.Unix() >= from {
			fmt.Fprintf(w, `{"traces":[{"traceID":"%032x","startTimeUnixNano":"%d","durationMs":3}]}`, 1, older.UnixNano())
			return
		}
		fmt.Fprint(w, `{"traces":[]}`)
	}
	got, ok, err = c.NewestTrace(ctx, AllSignals(), NewestWindow)
	if all, n := count(); err != nil || !ok || !got.Equal(older) || n != 3 {
		t.Errorf("old trace: %v %v %v after %v", got, ok, err, all)
	}
}

func TestAStoreThatHoldsNothingAnswersNone(t *testing.T) {
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"streams","result":[]}}`)
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"traces":[]}`) }
	c, ctx := f.client(), context.Background()
	if _, ok, err := c.NewestLog(ctx, AllSignals(), NewestWindow); ok || err != nil {
		t.Errorf("log: %v %v", ok, err)
	}
	if _, ok, err := c.NewestTrace(ctx, AllSignals(), NewestWindow); ok || err != nil {
		t.Errorf("trace: %v %v", ok, err)
	}
}

func TestTheNewestEntryIsOnlyForAScopeWithNoLimit(t *testing.T) {
	f := newFake(t)
	c, ctx := f.client(), context.Background()
	limited := Scope{Signals: Signals, Namespaces: []string{"team-a"}}
	if _, _, err := c.NewestLog(ctx, limited, NewestWindow); err == nil {
		t.Error("a namespace-limited scope read the newest log line")
	}
	if _, _, err := c.NewestTrace(ctx, limited, NewestWindow); err == nil {
		t.Error("a namespace-limited scope read the newest trace")
	}
	if len(f.paths()) != 0 {
		t.Errorf("the stores were asked: %v", f.paths())
	}
}
