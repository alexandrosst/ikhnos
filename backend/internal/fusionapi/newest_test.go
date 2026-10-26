package fusionapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
	if q := f.last("/api/search"); q.Get("limit") != "2" || q.Get("q") != "{ true }" {
		t.Errorf("asked Tempo for %v", q)
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
