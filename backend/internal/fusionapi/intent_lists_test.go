package fusionapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
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
		promMatch = append(promMatch, r.URL.Query()["match[]"]...)
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": []string{"redis"}}) // only redis has metrics, where it runs
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lokiQ = append(lokiQ, r.URL.Query().Get("query"))
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": []string{}})
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"traces": []any{}}) }
	per, sources, _, err := f.client().GroupSignals(context.Background(), AllSignals(), []AppGroup{shop}, rangeAll)
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
	if sig["redis"] != "metrics" || sig["api"] != "" || sources[SignalMetrics] != SourceOK {
		t.Fatalf("signals %v, sources %v", sig, sources)
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
