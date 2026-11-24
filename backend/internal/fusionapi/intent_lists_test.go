package fusionapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A line shown as an error is found as one: a container's stdout has no severity_text, only the level Loki detected.
func TestASeverityFilterFindsWhatALineIsShownWith(t *testing.T) {
	q, err := LogFilter{Severity: "error"}.logQL(AllSignals())
	if err != nil || !strings.Contains(q, `| (severity_text=~"(?i)error" or (severity_text="" and (detected_level=~"(?i)error" or level=~"(?i)error")))`) {
		t.Fatalf("%v %s", err, q)
	}
}

// shop has api in web on c1, and redis in data on c2. Every store is asked for the application's services place by place, and
// a member's signals in /applications are those it has where it runs: api of c2 having telemetry is not api of c1 having it.
func TestAnApplicationsServicesAreLookedForWhereEachRuns(t *testing.T) {
	shop := AppGroup{ID: "app", Name: "Shop", Members: []AppMember{{Name: "api", Namespace: "web", Cluster: "c1"}, {Name: "redis", Namespace: "data", Cluster: "c2"}}}
	f := newFake(t)
	var mu sync.Mutex
	var promMatch, lokiQ []string
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		promMatch = append(promMatch, formOf(r)["match[]"]...)
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": []string{"redis"}}) // only redis has metrics, where it runs
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lokiQ = append(lokiQ, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": []string{}})
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"traces": []any{}}) }
	per, sources, warnings, err := f.client().GroupSignals(context.Background(), AllSignals(), []AppGroup{shop}, rangeAll)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range promMatch {
		if strings.Contains(m, `"web"`) && strings.Contains(m, "redis") || strings.Contains(m, `"data"`) && strings.Contains(m, "api") {
			t.Fatalf("a name looked for in another member's place: %s", m)
		}
	}
	if len(lokiQ) != 2 || !strings.Contains(strings.Join(lokiQ, " "), `ikhnos_cluster_id="c2"`) {
		t.Fatalf("logs are read place by place: %v", lokiQ)
	}
	views := DescribeGroupsIn([]AppGroup{shop}, per)
	sig := map[string]string{}
	for _, sv := range views[0].Services {
		var s []string
		for _, x := range sv.Signals {
			s = append(s, string(x))
		}
		sig[sv.Name] = strings.Join(s, ",")
	}
	if sig["redis"] != "metrics" || sig["api"] != "" || sources[SignalMetrics] != SourceOK || sources[SignalLogs] != SourceOK {
		t.Fatalf("signals %v, sources %v, warnings %v", sig, sources, warnings)
	}
}

func TestATraceListingOfALongRangeSaysItReadTheNewestWeek(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"traces": []any{}}) }
	s := Scope{Signals: Signals, Namespaces: []string{"shop"}}
	long := TimeRange{From: rangeAll.To.Add(-14 * 24 * 3600e9), To: rangeAll.To}
	_, sample, err := f.client().traceServices(context.Background(), s, long)
	if err != nil || !strings.Contains(sample, "newest 7d") {
		t.Fatalf("%v %q", err, sample)
	}
}

// An application's log services are read place by place: a few places at a time, each with the caller's namespace
// rights on the selector beside the place's own namespace, and the place's cluster matched after the pipe, where a line
// that carries the cluster as structured metadata only is still found. The answer is the same whatever order the places
// come back in.
func TestAnApplicationsLogServicesAreReadPlaceByPlaceAFewAtATime(t *testing.T) {
	var focus []FocusPlace
	for i := range 10 {
		focus = append(focus, FocusPlace{Namespace: "web", Cluster: fmt.Sprintf("c%d", i),
			Names: []string{fmt.Sprintf("svc%d", 9-i)}})
	}
	focus = append(focus, FocusPlace{Namespace: "data", Names: []string{"redis"}})
	s := Scope{Signals: Signals, Namespaces: []string{"web", "data"}, Focus: focus}
	f := newFake(t)
	var mu sync.Mutex
	var queries []string
	inFlight, most := 0, 0
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		q := formOf(r).Get("query")
		mu.Lock()
		queries = append(queries, r.URL.Path+" "+q)
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, rest, _ := strings.Cut(q, `service_name=~"`)
		name, _, _ := strings.Cut(rest, `"`)
		writeJSON(w, map[string]any{"status": "success", "data": []string{name}})
	}
	names, sample, err := f.client().logServices(context.Background(), s, rangeAll)
	if err != nil || sample != "" {
		t.Fatalf("%v %q", err, sample)
	}
	if want := "redis,svc0,svc1,svc2,svc3,svc4,svc5,svc6,svc7,svc8,svc9"; strings.Join(names, ",") != want {
		t.Fatalf("services %v, want %s", names, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if most > logPlaceReads || most < 2 {
		t.Fatalf("%d places were read at once; want a few, at most %d", most, logPlaceReads)
	}
	for _, q := range queries {
		if !strings.Contains(q, `k8s_namespace_name=~"web|data"`) {
			t.Fatalf("a place was read without the caller's namespace rights: %s", q)
		}
		if !strings.HasPrefix(q, "/loki/api/v1/label/service_name/values ") {
			t.Fatalf("a place's services are read off the index: %s", q)
		}
		if !strings.Contains(q, `"redis"`) && !strings.Contains(q, `,ikhnos_cluster_id="c`) {
			t.Fatalf("a place's cluster is not in its selector: %s", q)
		}
	}
}

// A Loki that answers the label-values call with no list is a store fault, as it is for the listing of all services.
func TestAnApplicationsLogServicesRefuseALokiAnswerWithNoList(t *testing.T) {
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"status": "success"}) }
	s := Scope{Signals: Signals, Focus: []FocusPlace{{Namespace: "web", Names: []string{"api"}}}}
	if _, _, err := f.client().logServices(context.Background(), s, rangeAll); statusOf(err) != http.StatusBadGateway {
		t.Fatalf("an answer with no list was taken for none: %v", err)
	}
}

// One place whose read fails fails the list at once: the other places' reads are cancelled rather than waited for, and
// the error returned is the store's own, not the "context canceled" that cancelling them causes.
func TestAnApplicationsLogServicesStopAtTheFirstPlaceThatFails(t *testing.T) {
	f := newFake(t)
	var mu sync.Mutex
	cancelled := 0
	arrived := make(chan struct{}, logPlaceReads)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(formOf(r).Get("query"), `k8s_namespace_name="bad"`) {
			// Fail only once every slow read has reached the store, so that each is one the failure must cancel.
			for range logPlaceReads - 1 {
				select {
				case <-arrived:
				case <-time.After(5 * time.Second):
				}
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		arrived <- struct{}{}
		select {
		case <-r.Context().Done():
			mu.Lock()
			cancelled++
			mu.Unlock()
		case <-time.After(10 * time.Second):
		}
	}
	// As many places as are read at once, so that every slow read is running when the failing one fails.
	places := []FocusPlace{{Namespace: "slow0", Names: []string{"a"}}, {Namespace: "bad", Names: []string{"b"}}}
	for i := 1; len(places) < logPlaceReads; i++ {
		places = append(places, FocusPlace{Namespace: fmt.Sprintf("slow%d", i), Names: []string{"a"}})
	}
	start := time.Now()
	_, _, err := f.client().logServices(context.Background(), Scope{Signals: Signals, Focus: places}, rangeAll)
	if statusOf(err) != http.StatusBadGateway || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("the error is not the failing store's: %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the list waited %s for reads whose answers it could not use", took)
	}
	// The server sees a request cancelled once the client has closed its connection, which can be just after the call
	// returns.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := cancelled
		mu.Unlock()
		if n == logPlaceReads-1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of the %d slow reads were cancelled", n, logPlaceReads-1)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Only clusters the caller's rights limit it to make the log services a sample of the newest lines. An application's
// clusters, when it runs in too many places to name each, are a choice: the list stays complete, read off the index.
func TestFocusClustersDoNotMakeTheLogServicesASample(t *testing.T) {
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/label/service_name/values" {
			t.Errorf("the services were read from %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"status": "success", "data": []string{"api"}})
	}
	s := Scope{Signals: Signals, FocusServices: []string{"api"}, FocusNamespaces: []string{"web"},
		FocusClusters: []string{"c1", "c2"}}
	names, sample, err := f.client().logServices(context.Background(), s, rangeAll)
	if err != nil || sample != "" || len(names) != 1 || names[0] != "api" {
		t.Fatalf("%v %q %v", names, sample, err)
	}
	if q := f.last("/loki/api/v1/label/service_name/values").Get("query"); !strings.Contains(q, `,ikhnos_cluster_id=~"c1|c2"}`) {
		t.Fatalf("the application's clusters are not in the selector: %s", q)
	}
	// A cluster the rights limit the caller to still reads the newest lines, each checked against the rights.
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		stream := map[string]string{"service_name": "api", "k8s_namespace_name": "web", "ikhnos_cluster_id": "c1"}
		writeJSON(w, lokiResult([]any{stream, []any{"1791200000000000000", "x"}}))
	}
	rights := Scope{Signals: Signals, Clusters: []string{"c1"}}
	if _, _, err := f.client().logServices(context.Background(), rights, rangeAll); err != nil {
		t.Fatal(err)
	}
	f.last("/loki/api/v1/query_range")
}
