package fusionapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// storedLine is one line a lokiStore holds: its stream's labels and its structured metadata.
type storedLine struct {
	stream, meta map[string]string
	at           time.Time
	text         string
}

// lokiStore is a small Loki: it answers a query_range by the query's own selector and filters, newest first and cut to
// the limit, so that a read of several resources at once and a read of each can be checked against the same lines. It
// understands what the fused read sends: label matchers (= and =~) in the selector, label filters after a pipe, and a
// filter that is an alternation of conjunctions. Other stages (a line filter, a severity) are not used by these tests.
type lokiStore struct {
	mu      sync.Mutex
	lines   []storedLine
	queries []string
	methods []string
	// fail, when it gives a status, answers the query with it.
	fail func(q string) int
}

var matcherRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(=~|!=|=)("(?:[^"\\]|\\.)*")`)

// labelCond is one matcher or label filter.
type labelCond struct {
	key, op string
	re      *regexp.Regexp
	val     string
}

func (c labelCond) holds(labels map[string]string) bool {
	v := labels[c.key]
	switch c.op {
	case "=":
		return v == c.val
	case "!=":
		return v != c.val
	}
	return c.re.MatchString(v)
}

func parseConds(t *testing.T, s string) []labelCond {
	var out []labelCond
	for _, m := range matcherRe.FindAllStringSubmatch(s, -1) {
		v, err := strconv.Unquote(m[3])
		if err != nil {
			t.Errorf("a value that is not quoted right: %s", m[3])
		}
		c := labelCond{key: m[1], op: m[2], val: v}
		if c.op == "=~" {
			c.re = regexp.MustCompile("^(?:" + v + ")$")
		}
		out = append(out, c)
	}
	return out
}

func (st *lokiStore) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() // (a long question comes as a form)
		q := r.Form
		query := q.Get("query")
		st.mu.Lock()
		defer st.mu.Unlock()
		st.queries, st.methods = append(st.queries, query), append(st.methods, r.Method)
		if code := 0; st.fail != nil {
			if code = st.fail(query); code != 0 {
				http.Error(w, `{"error":"boom"}`, code)
				return
			}
		}
		end := strings.Index(query, "}")
		selector := parseConds(t, query[:end])
		// Each stage after a pipe must hold; a stage that is an alternation holds when one of its conjunctions does.
		var stages [][][]labelCond
		for _, stage := range strings.Split(query[end+1:], " | ")[1:] {
			var alts [][]labelCond
			for _, alt := range strings.Split(stage, " or ") {
				alts = append(alts, parseConds(t, alt))
			}
			stages = append(stages, alts)
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		var hits []storedLine
		for _, l := range st.lines {
			all := map[string]string{}
			for k, v := range l.meta {
				all[k] = v
			}
			for k, v := range l.stream { // a stream label wins over metadata of the same name, as in Loki
				all[k] = v
			}
			ok := true
			for _, c := range selector {
				ok = ok && c.holds(l.stream)
			}
			for _, alts := range stages {
				any := false
				for _, conj := range alts {
					holds := true
					for _, c := range conj {
						holds = holds && c.holds(all)
					}
					any = any || holds
				}
				ok = ok && any
			}
			if ok {
				hits = append(hits, l)
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.After(hits[j].at) })
		if len(hits) > limit {
			hits = hits[:limit]
		}
		var entries [][]any
		for _, l := range hits {
			entries = append(entries, []any{l.stream, []any{strconv.FormatInt(l.at.UnixNano(), 10), l.text,
				map[string]any{"structuredMetadata": l.meta}}})
		}
		writeJSON(w, lokiResult(entries...))
	}
}

func (st *lokiStore) asked() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.queries...)
}

func (st *lokiStore) reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.queries, st.methods = nil, nil
}

// lbl is a label set written as name, value, name, value ...
func lbl(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// testRes is a resource of a test trace: its service, namespace, pod, node and cluster ("" for none).
type testRes struct{ service, ns, pod, node, cluster string }

// tempoTraceOf is a trace of one span on each resource, the spans a millisecond apart.
func tempoTraceOf(res ...testRes) map[string]any {
	base := time.Date(2026, 10, 5, 11, 30, 0, 0, time.UTC).UnixNano()
	var rss []any
	for i, r := range res {
		var attrs []any
		for _, a := range [][2]string{{"service.name", r.service}, {"k8s.namespace.name", r.ns}, {"k8s.pod.name", r.pod},
			{"k8s.node.name", r.node}, {"ikhnos.cluster.id", r.cluster}} {
			if a[1] != "" {
				attrs = append(attrs, kv(a[0], a[1]))
			}
		}
		start := base + int64(i)*1e6
		rss = append(rss, map[string]any{"resource": map[string]any{"attributes": attrs},
			"scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"traceId": traceHex,
				"spanId": strings.Repeat("0", 14) + strconv.Itoa(10+i), "name": "op",
				"startTimeUnixNano": json.Number(itoa(start)), "endTimeUnixNano": json.Number(itoa(start + 1e6))}}}}})
	}
	return map[string]any{"trace": map[string]any{"resourceSpans": rss}}
}

// at is a time n seconds after the test traces' start, inside their padded window.
func at(n int) time.Time {
	return time.Date(2026, 10, 5, 11, 30, 0, 0, time.UTC).Add(time.Duration(n) * time.Second)
}

// appLines are n lines of a pod's stream, the newest at newest seconds and one second apart going back.
func appLines(service, ns, pod, cluster string, newest, n int, meta map[string]string) []storedLine {
	var out []storedLine
	for i := 0; i < n; i++ {
		stream := map[string]string{"service_name": service, "k8s_namespace_name": ns, "k8s_pod_name": pod}
		if cluster != "" {
			stream["ikhnos_cluster_id"] = cluster
		}
		out = append(out, storedLine{stream: stream, meta: meta, at: at(newest - i), text: pod + " line " + strconv.Itoa(i)})
	}
	return out
}

// contextLogsOneByOne is what each resource's own read of its context lines returns: the answer before the reads were
// put together, to compare with.
func contextLogsOneByOne(t *testing.T, c *Client, s Scope, got *Fused, opts FuseOptions) map[string][2]any {
	t.Helper()
	if err := opts.defaults(); err != nil {
		t.Fatal(err)
	}
	run := &fuseRun{c: c, s: s, opts: opts}
	window, _ := windowAround(got.Trace, opts.Pad, MaxLogWindow)
	want := map[string][2]any{}
	for _, res := range got.Resources {
		lines, truncated, err := c.Logs(context.Background(), s, run.contextFilter(res), window, opts.MaxContextLogs)
		if err != nil {
			t.Fatal(err)
		}
		reverseLogs(lines)
		want[res.Key] = [2]any{lines, truncated}
	}
	return want
}

func contextLogsOf(got *Fused) map[string][2]any {
	out := map[string][2]any{}
	for _, res := range got.Resources {
		out[res.Key] = [2]any{res.Logs, res.LogsTruncated}
	}
	return out
}

// The context lines of all the resources are one read; each resource still gets what its own read returned, including
// the lines of a resource that names no pod (every pod's), the cut to max_context_logs, and a line the caller may not be
// given that took a place in a resource's limit all the same.
func TestTheContextLinesOfAllTheResourcesAreOneRead(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tempoTraceOf(testRes{"cart", "shop", "cart-1", "", "cl-1"}, testRes{"cart", "shop", "cart-2", "", "cl-1"},
			testRes{"cart", "shop", "", "", "cl-1"}, testRes{"billing", "pay", "billing-7", "", "cl-2"}))
	}
	st := &lokiStore{}
	st.lines = append(st.lines, appLines("cart", "shop", "cart-1", "cl-1", 10, 6, nil)...)
	st.lines = append(st.lines, appLines("cart", "shop", "cart-2", "cl-1", 20, 2, nil)...)
	// Another cluster's cart, and lines of the trace itself, which are not context.
	st.lines = append(st.lines, appLines("cart", "shop", "cart-9", "cl-9", 30, 3, nil)...)
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-2", 5, 3, lbl("trace_id", traceHex))...)
	// The newest of billing's context lines claims, in its metadata, to be from another cluster than its stream says: a
	// caller limited to clusters is not given it, yet it took one of the places billing's own read asked for.
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-2", 40, 1, lbl("ikhnos_cluster_id", "cl-9"))...)
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-2", 39, 5, nil)...)
	f.loki = st.handler(t)
	c := f.client()
	opts := FuseOptions{ContextLogs: true, MaxContextLogs: 3}
	for _, s := range []Scope{AllSignals(), {Signals: Signals, Clusters: []string{"cl-1", "cl-2"}}} {
		st.reset()
		got, err := c.FuseTrace(context.Background(), s, traceHex, opts)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(st.asked()); n != 1 {
			t.Fatalf("%d reads of Loki for 4 resources: %v", n, st.asked())
		}
		if got.Sources[SourceContextLogs] != SourceOK {
			t.Fatalf("sources = %v %v", got.Sources, got.Warnings)
		}
		if !strings.Contains(st.asked()[0], `{service_name=~"cart|billing",k8s_namespace_name=~"shop|pay"}`) ||
			!strings.Contains(st.asked()[0], `(service_name="cart" and k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1")`) {
			t.Fatalf("query = %s", st.asked()[0])
		}
		want := contextLogsOneByOne(t, c, s, got, opts)
		if have := contextLogsOf(got); !reflect.DeepEqual(have, want) {
			t.Fatalf("scope %+v:\nat once:    %+v\none by one: %+v", s, have, want)
		}
		// (What is compared is not empty: cart-1 is cut, and the cart that names no pod has the lines of both of its pods.)
		if r := got.Resources[0]; len(r.Logs) != 3 || !r.LogsTruncated || len(got.Resources[2].Logs) != 3 {
			t.Fatalf("cart-1 %+v, cart %+v", r.Logs, got.Resources[2].Logs)
		}
	}
}

// When Loki's answer to the read of every resource is full, a resource with fewer lines in it than its own read would
// have asked for may have had more further back: those resources, and only those, are read on their own.
func TestAResourceTheFullAnswerCannotTellIsReadOnItsOwn(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tempoTraceOf(testRes{"cart", "shop", "cart-1", "", "cl-1"},
			testRes{"billing", "pay", "billing-7", "", "cl-1"}, testRes{"audit", "secret", "audit-0", "", "cl-1"}))
	}
	st := &lokiStore{}
	// max 2 for 3 resources asks for 9 lines: billing's one line and cart's 8 newest fill them, audit's lines are older than
	// all of those.
	st.lines = append(st.lines, appLines("cart", "shop", "cart-1", "cl-1", 100, 20, nil)...)
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-1", 101, 1, nil)...)
	st.lines = append(st.lines, appLines("audit", "secret", "audit-0", "cl-1", 50, 4, nil)...)
	f.loki = st.handler(t)
	c := f.client()
	opts := FuseOptions{ContextLogs: true, MaxContextLogs: 2}
	got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	asked := st.asked()
	if len(asked) != 3 || !strings.Contains(asked[0], " or ") || strings.Contains(asked[1], " or ") ||
		strings.Contains(asked[2], " or ") {
		t.Fatalf("reads = %v", asked)
	}
	if strings.Contains(strings.Join(asked[1:], "\n"), `"cart"`) {
		t.Fatalf("cart, which the full answer told exactly, was read again: %v", asked)
	}
	st.reset()
	want := contextLogsOneByOne(t, c, AllSignals(), got, opts)
	if have := contextLogsOf(got); !reflect.DeepEqual(have, want) {
		t.Fatalf("at once:    %+v\none by one: %+v", have, want)
	}
	if r := got.Resources; !r[0].LogsTruncated || r[1].LogsTruncated || !r[2].LogsTruncated || len(r[2].Logs) != 2 {
		t.Fatalf("truncated: %+v", got.Resources)
	}
}

// batchesOnly answers a query that reads several items at once (one with "or" in it) with code.
func batchesOnly(code int) func(string) int {
	return func(q string) int {
		if strings.Contains(q, " or ") {
			return code
		}
		return 0
	}
}

// A read of every resource that Loki refuses for what it is (a 4xx: a query too long for it, say) is not the end of
// their context lines: each is read on its own, as before, so the sources and warnings are what reads one by one give,
// and a resource no query can be made for fails alone.
func TestARefusedReadOfAllTheResourcesFallsBackToOneByOne(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tempoTraceOf(testRes{"cart", "shop", "cart-1", "", "cl-1"},
			testRes{"billing", "pay", "billing-7", "", "cl-1"}, testRes{"bad\x01name", "pay", "x-1", "", "cl-1"}))
	}
	st := &lokiStore{fail: batchesOnly(http.StatusBadRequest)}
	st.lines = append(st.lines, appLines("cart", "shop", "cart-1", "cl-1", 10, 3, nil)...)
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-1", 10, 1, nil)...)
	f.loki = st.handler(t)
	c := f.client()
	opts := FuseOptions{ContextLogs: true, MaxContextLogs: 2}
	got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(st.asked()); n != 3 {
		t.Fatalf("reads = %v", st.asked())
	}
	if got.Sources[SourceContextLogs] != SourceError || len(got.Warnings) != 1 ||
		!strings.Contains(got.Warnings[0], "contextLogs: service") {
		t.Fatalf("sources %v, warnings %v", got.Sources, got.Warnings)
	}
	if len(got.Resources[0].Logs) != 2 || !got.Resources[0].LogsTruncated || len(got.Resources[1].Logs) != 1 {
		t.Fatalf("resources = %+v", got.Resources)
	}
}

// A read of every resource that fails as each own read would have (a store that answers with an error of its own, or
// one too slow to answer) is not repeated resource by resource, which would only spend the time again: the failure is
// reported for each resource, as reads one by one that all failed so reported it (the answer says each warning once).
func TestAFailedReadOfAllTheResourcesIsEachResourcesFailure(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tempoTraceOf(testRes{"cart", "shop", "cart-1", "", "cl-1"},
			testRes{"billing", "pay", "billing-7", "", "cl-1"}, testRes{"bad\x01name", "pay", "x-1", "", "cl-1"}))
	}
	for _, tc := range []struct {
		name, says string
		store      http.HandlerFunc
	}{
		{"an error of the store's", "Loki had a problem answering (status 500)",
			(&lokiStore{fail: func(string) int { return http.StatusInternalServerError }}).handler(t)},
		{"a store that hangs", "Loki took too long", hang},
	} {
		var mu sync.Mutex
		reads := 0
		f.loki = func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			reads++
			mu.Unlock()
			tc.store(w, r)
		}
		c := f.client()
		c.StoreTimeout = 100 * time.Millisecond
		got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{ContextLogs: true})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		n := reads
		mu.Unlock()
		var store, bad int
		for _, w := range got.Warnings {
			switch {
			case strings.Contains(w, "contextLogs: "+tc.says):
				store++
			case strings.Contains(w, "contextLogs: service"):
				bad++
			}
		}
		if n != 1 || got.Sources[SourceContextLogs] != SourceError || store != 1 || bad != 1 || len(got.Warnings) != 2 {
			t.Fatalf("%s: %d reads, sources %v, warnings %v", tc.name, n, got.Sources, got.Warnings)
		}
	}
}

// The read of several resources' context lines opens no more streams than their own reads would: their pods are in the
// selector when each names one, and a resource whose own read selects more widely than its service and namespace (a pod
// with no service, a service in no namespace) is read on its own, its lines what that read gives. A long question goes to
// Loki as a form.
func TestTheContextLinesReadAtOnceOpenOnlyTheResourcesStreams(t *testing.T) {
	f := newFake(t)
	res := []testRes{{"cart", "shop", "cart-1", "", "cl-1"}, {"billing", "pay", "billing-7", "", "cl-1"},
		{"", "shop", "lonely-0", "", "cl-1"}, {"audit", "", "audit-0", "", "cl-1"}}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTraceOf(res...)) }
	st := &lokiStore{}
	st.lines = append(st.lines, appLines("cart", "shop", "cart-1", "cl-1", 10, 3, nil)...)
	st.lines = append(st.lines, appLines("billing", "pay", "billing-7", "cl-1", 20, 2, nil)...)
	st.lines = append(st.lines, appLines("lonely", "shop", "lonely-0", "cl-1", 30, 2, nil)...)
	st.lines = append(st.lines, appLines("audit", "secret", "audit-0", "cl-1", 40, 2, nil)...)
	f.loki = st.handler(t)
	c := f.client()
	opts := FuseOptions{ContextLogs: true, MaxContextLogs: 5}
	got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	asked := st.asked()
	var batch []string
	for _, q := range asked {
		if strings.Contains(q, " or ") {
			batch = append(batch, q)
		}
	}
	if len(asked) != 3 || len(batch) != 1 ||
		!strings.HasPrefix(batch[0], `{service_name=~"cart|billing",k8s_pod_name=~"cart-1|billing-7",k8s_namespace_name=~"shop|pay"}`) {
		t.Fatalf("reads = %v", asked)
	}
	st.reset()
	want := contextLogsOneByOne(t, c, AllSignals(), got, opts)
	if have := contextLogsOf(got); !reflect.DeepEqual(have, want) {
		t.Fatalf("at once:    %+v\none by one: %+v", have, want)
	}
	if len(got.Resources[2].Logs) != 2 || len(got.Resources[3].Logs) != 2 {
		t.Fatalf("the resources read on their own: %+v", got.Resources[2:])
	}

	// Twenty resources with long names make a question longer than a URL should carry.
	res = nil
	for i := 0; i < maxFusedResources; i++ {
		n := fmt.Sprintf("a-service-with-a-rather-long-name-%02d", i)
		res = append(res, testRes{n, "shop", n + "-pod", "", "cl-1"})
	}
	st.reset()
	if _, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.queries) != 1 || st.methods[0] != http.MethodPost || !strings.Contains(st.queries[0], "rather-long-name-19") {
		t.Fatalf("%v %v", st.methods, st.queries)
	}
}

// systemLogsOneByOne is what the system lines were when each node was read on its own and the answers merged.
func systemLogsOneByOne(t *testing.T, c *Client, s Scope, locs []sysLoc, opts FuseOptions) *SystemLogs {
	t.Helper()
	sl := &SystemLogs{Namespaces: opts.SystemNamespaces, Nodes: []string{}, Entries: []LogEntry{}}
	for _, l := range locs {
		f := LogFilter{Namespaces: opts.SystemNamespaces, Node: l.node, Cluster: l.cluster, Backward: true}
		lines, trunc, err := c.Logs(context.Background(), s, f, rangeAll, opts.MaxSystemLogs)
		if err != nil {
			t.Fatal(err)
		}
		sl.Nodes = append(sl.Nodes, l.node)
		sl.Entries = append(sl.Entries, lines...)
		sl.Truncated = sl.Truncated || trunc
	}
	sort.Strings(sl.Nodes)
	sort.SliceStable(sl.Entries, func(i, j int) bool { return sl.Entries[i].Time.After(sl.Entries[j].Time) })
	if len(sl.Entries) > opts.MaxSystemLogs {
		sl.Entries, sl.Truncated = sl.Entries[:opts.MaxSystemLogs], true
	}
	reverseLogs(sl.Entries)
	return sl
}

// nodeLines are n kube-system lines of a node, the newest at newest seconds and three seconds apart going back (so that
// no two nodes' lines in these tests are written at the same time, which would leave their order to the store).
func nodeLines(node, cluster string, newest, n int, meta map[string]string) []storedLine {
	var out []storedLine
	for i := 0; i < n; i++ {
		m := map[string]string{"k8s_node_name": node}
		for k, v := range meta {
			m[k] = v
		}
		out = append(out, storedLine{stream: map[string]string{"service_name": "kubelet", "k8s_namespace_name": "kube-system",
			"ikhnos_cluster_id": cluster}, meta: m, at: at(newest - 3*i), text: node + " line " + strconv.Itoa(i)})
	}
	return out
}

// The system lines of the trace's nodes are one read, and the answer is the one a read per node, merged, gives: the
// newest max_system_logs of them all and whether there were more. Where that could differ (a full answer holding a line
// the caller may not be given, or one node named both with and without a cluster) the nodes are read one by one.
func TestTheSystemLinesOfAllTheNodesAreOneRead(t *testing.T) {
	f := newFake(t)
	st := &lokiStore{}
	st.lines = append(st.lines, nodeLines("n1", "cl-1", 100, 3, nil)...)
	st.lines = append(st.lines, nodeLines("n2", "cl-1", 99, 3, nil)...)
	st.lines = append(st.lines, nodeLines("n1", "cl-2", 98, 3, nil)...) // the same name in another cluster
	st.lines = append(st.lines, nodeLines("n3", "cl-2", 101, 1, map[string]string{"ikhnos_cluster_id": "cl-9"})...)
	f.loki = st.handler(t)
	c := f.client()
	clusters := Scope{Signals: Signals, Clusters: []string{"cl-1", "cl-2"}}
	n1, n2 := &Resource{Node: "n1", Cluster: "cl-1"}, &Resource{Node: "n2", Cluster: "cl-1"}
	n3, n1b := &Resource{Node: "n3", Cluster: "cl-2"}, &Resource{Node: "n1", Cluster: "cl-2"}
	for _, tc := range []struct {
		name  string
		s     Scope
		res   []*Resource
		max   int
		reads int
	}{
		{"not full", AllSignals(), []*Resource{n1, n2}, 10, 1},
		{"full", AllSignals(), []*Resource{n1, n2, n1b}, 4, 1},
		{"full, all of it the caller's", clusters, []*Resource{n1, n2}, 4, 1},
		{"not full, with a line the caller may not see", clusters, []*Resource{n1, n3}, 10, 1},
		{"full, with a line the caller may not see", clusters, []*Resource{n3, n1}, 2, 3},
		{"a node with and without its cluster", AllSignals(), []*Resource{n1, {Node: "n1"}}, 10, 2},
	} {
		st.reset()
		opts := FuseOptions{SystemLogs: true, MaxSystemLogs: tc.max}
		if err := opts.defaults(); err != nil {
			t.Fatal(err)
		}
		sl, warns, err := c.systemLogs(context.Background(), tc.s, tc.res, opts, rangeAll, make(chan struct{}, 2))
		if err != nil || len(warns) != 0 {
			t.Fatalf("%s: %v %v", tc.name, err, warns)
		}
		if n := len(st.asked()); n != tc.reads {
			t.Fatalf("%s: %d reads: %v", tc.name, n, st.asked())
		}
		var locs []sysLoc
		for _, r := range tc.res {
			locs = append(locs, sysLoc{r.Cluster, r.Node})
		}
		if want := systemLogsOneByOne(t, c, tc.s, locs, opts); !reflect.DeepEqual(sl, want) {
			t.Fatalf("%s:\nat once:    %+v\none by one: %+v", tc.name, sl, want)
		}
		if len(sl.Entries) == 0 {
			t.Fatalf("%s: nothing to compare", tc.name)
		}
	}
	// The selector is the system namespaces' and the Scope's, as a read of one node has it.
	st.reset()
	opts := FuseOptions{SystemLogs: true}
	_ = opts.defaults()
	res := []*Resource{n1, {Node: "n2"}}
	if _, _, err := c.systemLogs(context.Background(), clusters, res, opts, rangeAll, make(chan struct{}, 1)); err != nil {
		t.Fatal(err)
	}
	want := `{service_name=~".+",k8s_namespace_name=~"kube-system"} | ikhnos_cluster_id=~"cl-1|cl-2" | ` +
		`(k8s_node_name="n1" and ikhnos_cluster_id="cl-1") or (k8s_node_name="n2")`
	if q := st.asked(); len(q) != 1 || q[0] != want {
		t.Fatalf("query = %v", q)
	}
}

// slowMatch is how a resource was matched before the topology was indexed: a pass over every service.
func slowMatch(v *TopologyView, r *Resource) (*TopoService, bool) {
	if r.Service == "" {
		return nil, false
	}
	var byName, byLabel []*TopoService
	for i := range v.Services {
		c := &v.Services[i]
		if (r.Namespace != "" && c.Namespace != r.Namespace) || (r.Cluster != "" && c.Cluster != r.Cluster) {
			continue
		}
		if c.Name == r.Service {
			byName = append(byName, c)
		} else if c.Labels["app"] == r.Service || c.Labels["app.kubernetes.io/name"] == r.Service {
			byLabel = append(byLabel, c)
		}
	}
	for _, found := range [][]*TopoService{byName, byLabel} {
		switch len(found) {
		case 0:
		case 1:
			return found[0], false
		default:
			return nil, true
		}
	}
	return nil, false
}

// slowNeighbours is a resource's neighbours as they were found before: a pass over every link.
func slowNeighbours(v *TopologyView, s Scope, r *Resource) *ResourceTopology {
	sv, _ := slowMatch(v, r)
	if sv == nil {
		return nil
	}
	byID := v.byID()
	neighbour := func(id, kind string, l TopoLink) (Neighbour, bool) {
		n := Neighbour{ID: id, Kind: kind, Protocol: l.Protocol, Port: l.Port, Confidence: l.Confidence, Stale: l.Stale}
		if kind == "external" {
			if n.Name = v.Externals[id]; n.Name == "" {
				n.Name = id
			}
		} else if o := byID[id]; o != nil {
			n.Name, n.Namespace, n.Cluster = o.Name, o.Namespace, o.Cluster
		}
		return n, endVisible(s, byID, id, kind)
	}
	rt := &ResourceTopology{Service: sv, Calls: []Neighbour{}, CalledBy: []Neighbour{}}
	for _, l := range v.Links {
		if l.Noise != "" {
			continue
		}
		switch {
		case l.From == sv.ID && len(rt.Calls) < maxNeighbours:
			if n, ok := neighbour(l.To, l.ToKind, l); ok {
				rt.Calls = append(rt.Calls, n)
			}
		case l.To == sv.ID && l.FromKind != "external" && len(rt.CalledBy) < maxNeighbours:
			if n, ok := neighbour(l.From, l.FromKind, l); ok {
				rt.CalledBy = append(rt.CalledBy, n)
			}
		}
	}
	for _, l := range [][]Neighbour{rt.Calls, rt.CalledBy} {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Name != l[j].Name {
				return l[i].Name < l[j].Name
			}
			return l[i].ID < l[j].ID
		})
	}
	return rt
}

// The topology is indexed once for all the resources of a read; every resource is matched, and gets the neighbours, it
// got from a pass over the whole topology: names and app labels alike, services of one name in several places, a service
// that calls itself, links past maxNeighbours, noise and addresses outside the clusters, and a Scope that hides some.
func TestTheIndexedTopologyMatchesAsAPassOverItDid(t *testing.T) {
	v := &TopologyView{Externals: map[string]string{"x-0": "GitHub"}}
	names := []string{"cart", "billing", "web", "db"}
	for i := 0; i < 40; i++ {
		sv := TopoService{ID: "s-" + strconv.Itoa(i), Name: names[i%4] + strconv.Itoa(i%7),
			Namespace: []string{"shop", "pay", "secret"}[i%3], Cluster: []string{"cl-1", "cl-2"}[i%2], Labels: map[string]string{}}
		if i%5 == 0 {
			sv.Labels["app"] = names[i%4] + "0"
		}
		if i%6 == 0 {
			sv.Labels["app.kubernetes.io/name"] = names[(i+1)%4] + "1"
		}
		v.Services = append(v.Services, sv)
	}
	for i := 0; i < 4000; i++ {
		l := TopoLink{ID: "l-" + strconv.Itoa(i), From: "s-" + strconv.Itoa(i%40), To: "s-" + strconv.Itoa((i*7)%41),
			FromKind: "service", ToKind: "service", Port: i}
		switch i % 9 {
		case 0:
			l.To, l.ToKind = "x-"+strconv.Itoa(i%2), "external"
		case 1:
			l.Noise = "dns"
		case 2:
			l.To = l.From
		case 3:
			l.From, l.FromKind = "x-0", "external"
		}
		v.Links = append(v.Links, l)
	}
	ix := newTopoIndex(v)
	matched, full, ambiguous := 0, false, false
	scopes := []Scope{AllSignals(), {Signals: Signals, Namespaces: []string{"shop"}},
		{Signals: Signals, Clusters: []string{"cl-2"}}}
	for _, s := range scopes {
		for _, svc := range append(names, "cart0", "cart1", "billing1", "web2", "db3", "db0", "none") {
			for _, ns := range []string{"", "shop", "pay"} {
				for _, cl := range []string{"", "cl-1", "cl-2"} {
					r := &Resource{Service: svc, Namespace: ns, Cluster: cl}
					sv, amb := ix.matchService(r)
					wsv, wamb := slowMatch(v, r)
					if sv != wsv || amb != wamb {
						t.Fatalf("%+v: matched %v %v, a pass over the services %v %v", r, sv, amb, wsv, wamb)
					}
					got, want := ix.topologyFor(s, r), slowNeighbours(v, s, r)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%+v: neighbours\n%+v\nwant\n%+v", r, got, want)
					}
					if sv != nil {
						matched++
						full = full || len(got.Calls) == maxNeighbours
					}
					ambiguous = ambiguous || amb
				}
			}
		}
	}
	if matched == 0 || !full || !ambiguous {
		t.Fatalf("not everything was compared: %d matched, a full list %v, an ambiguous name %v", matched, full, ambiguous)
	}
}

// promStore is a small Prometheus: it answers a range query that is one selector or several joined with "or" with every
// series one of them matches, sorted by their labels and cut to the limit, as Prometheus 3.2 and later do. Questions
// come in the URL or, when long, as a form.
type promStore struct {
	mu      sync.Mutex
	series  []map[string]string
	asked   []string
	methods []string
	fail    func(q string) int // a status to answer the query with, or 0
}

func (st *promStore) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.FormValue("query")
		st.mu.Lock()
		defer st.mu.Unlock()
		st.asked, st.methods = append(st.asked, q), append(st.methods, r.Method)
		if code := 0; st.fail != nil {
			if code = st.fail(q); code != 0 {
				http.Error(w, `{"error":"boom"}`, code)
				return
			}
		}
		expr, err := parser.ParseExpr(q)
		if err != nil {
			t.Errorf("not a query: %s: %v", q, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var sels [][]*labels.Matcher
		parser.Inspect(expr, func(n parser.Node, _ []parser.Node) error {
			if vs, ok := n.(*parser.VectorSelector); ok {
				sels = append(sels, vs.LabelMatchers)
			}
			return nil
		})
		var hits []map[string]string
		for _, s := range st.series {
			if slices.ContainsFunc(sels, func(ms []*labels.Matcher) bool { return matchesAll(ms, s) }) {
				hits = append(hits, s)
			}
		}
		sort.Slice(hits, func(i, j int) bool { return labels.Compare(labels.FromMap(hits[i]), labels.FromMap(hits[j])) < 0 })
		if limit, _ := strconv.Atoi(r.FormValue("limit")); limit > 0 && len(hits) > limit {
			hits = hits[:limit]
		}
		var res []any
		for _, h := range hits {
			res = append(res, map[string]any{"metric": h, "values": [][]any{{1791200000.0, "1"}}})
		}
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": res}})
	}
}

func (st *promStore) reads() ([]string, []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.asked...), append([]string(nil), st.methods...)
}

// series are n series of one view's labels, named prefix_00, prefix_01 ...
func series(prefix string, n int, lbls map[string]string) []map[string]string {
	var out []map[string]string
	for i := 0; i < n; i++ {
		m := map[string]string{"__name__": fmt.Sprintf("%s_%02d", prefix, i)}
		for k, v := range lbls {
			m[k] = v
		}
		out = append(out, m)
	}
	return out
}

// metricsOneByOne is what each resource's series were when each view was read on its own.
func metricsOneByOne(t *testing.T, c *Client, s Scope, opts FuseOptions) map[string][2]any {
	t.Helper()
	if err := opts.defaults(); err != nil {
		t.Fatal(err)
	}
	tr, err := c.Trace(context.Background(), s, traceHex)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.newFuseRun(context.Background(), s, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	reads := newMetricReads(c, s, run.window, run.step, opts.MaxSeries)
	want := map[string][2]any{}
	for _, res := range run.looked {
		series, truncated, err := resourceMetrics(context.Background(), res, opts, reads)
		if err != nil {
			t.Fatal(err)
		}
		want[res.Key] = [2]any{series, truncated}
	}
	return want
}

// The metric views of all the resources are one range query, and each resource carries the series its own views' reads
// gave: a view cut by its limit, a series two views of one resource share, and, when the answer is full, the views it
// cannot tell read on their own. A query Prometheus refuses for what it is is each view's own read again; one that fails
// otherwise fails every view, as each view's own read would have, and is not read again.
func TestTheMetricViewsOfAllTheResourcesAreOneRead(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tempoTraceOf(testRes{"cart", "shop", "cart-1", "n1", "cl-1"},
			testRes{"billing", "pay", "billing-7", "n1", "cl-1"}))
	}
	st := &promStore{}
	ns, cl := "k8s_namespace_name", "ikhnos_cluster_id"
	st.series = append(st.series, series("a", 10, lbl("service_name", "cart", ns, "shop", cl, "cl-1"))...)
	st.series = append(st.series, series("b", 5, lbl("service_name", "billing", ns, "pay", cl, "cl-1"))...)
	// cart-1's own series, one of which also names the service (it is in both of cart's views, and kept once).
	st.series = append(st.series, series("z", 1, lbl(ns, "shop", "k8s_pod_name", "cart-1", cl, "cl-1"))...)
	st.series = append(st.series, series("y", 1, lbl("service_name", "cart", ns, "shop", "k8s_pod_name", "cart-1", cl, "cl-1"))...)
	st.series = append(st.series, series("a", 3, lbl("service_name", "cart", ns, "shop", cl, "cl-9"))...)
	f.prom = st.handler(t)
	c := f.client()
	for _, tc := range []struct {
		name      string
		maxSeries int
		views     MetricViews
		reads     int
		fail      int
	}{
		// 4 views of 6 places each: 24 > the 17 series of these clusters, so all of them are in the answer.
		{"not full", 5, MetricViews{}, 1, 0},
		// 4 views of 3 places: the answer is cut after a_00 .. a_09 and b_00, b_01; cart's service view is told by it, the
		// other three are read on their own.
		{"full", 2, MetricViews{}, 4, 0},
		{"with the node", 4, MetricViews{App: true, Pod: true, Node: true}, 1, 0},
		{"a refused query", 5, MetricViews{}, 5, http.StatusBadRequest},
		{"a failed query", 5, MetricViews{}, 1, http.StatusInternalServerError},
	} {
		st.mu.Lock()
		st.asked, st.methods = nil, nil
		st.fail = nil
		if tc.fail != 0 {
			st.fail = batchesOnly(tc.fail)
		}
		st.mu.Unlock()
		opts := FuseOptions{Metrics: true, MaxSeries: tc.maxSeries, MetricViews: tc.views}
		got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts)
		if err != nil {
			t.Fatal(err)
		}
		asked, _ := st.reads()
		if len(asked) != tc.reads || !strings.Contains(asked[0], " or ") {
			t.Fatalf("%s: %d reads: %v", tc.name, len(asked), asked)
		}
		if tc.fail == http.StatusInternalServerError {
			// Each resource reports the failure, as when its own reads failed so (the answer says it once); none carries a
			// series.
			if got.Sources[SignalMetrics] != SourceError || len(got.Warnings) != 1 ||
				got.Warnings[0] != "metrics: Prometheus had a problem answering (status 500)" || len(got.Resources[0].Metrics) != 0 {
				t.Fatalf("%s: %v %v", tc.name, got.Sources, got.Warnings)
			}
			continue
		}
		if got.Sources[SignalMetrics] != SourceOK || len(got.Warnings) != 0 {
			t.Fatalf("%s: %v %v", tc.name, got.Sources, got.Warnings)
		}
		have := map[string][2]any{}
		for _, r := range got.Resources {
			have[r.Key] = [2]any{r.Metrics, r.MetricsTruncated}
		}
		st.mu.Lock()
		st.fail = nil
		st.mu.Unlock()
		if want := metricsOneByOne(t, c, AllSignals(), opts); !reflect.DeepEqual(have, want) {
			t.Fatalf("%s:\nat once:    %+v\none by one: %+v", tc.name, have, want)
		}
		if cart := got.Resources[0]; len(cart.Metrics) == 0 || !cart.MetricsTruncated {
			t.Fatalf("%s: nothing much was compared: %+v", tc.name, cart)
		}
	}
}

// A trace of many resources asks a long question; it goes to Prometheus as a form, not in a URL a proxy may refuse.
func TestALongQuestionOfTheMetricViewsIsSentAsAForm(t *testing.T) {
	f := newFake(t)
	var res []testRes
	for i := 0; i < maxFusedResources; i++ {
		res = append(res, testRes{"service-" + strconv.Itoa(i), "shop", "pod-" + strconv.Itoa(i), "", "cl-1"})
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTraceOf(res...)) }
	up := lbl("service_name", "service-3", "k8s_namespace_name", "shop", "ikhnos_cluster_id", "cl-1")
	st := &promStore{series: series("up", 1, up)}
	f.prom = st.handler(t)
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Metrics: true})
	if err != nil {
		t.Fatal(err)
	}
	asked, methods := st.reads()
	if len(asked) != 1 || methods[0] != http.MethodPost || len(asked[0]) <= maxGetQuery {
		t.Fatalf("%d reads, %v, %d characters", len(asked), methods, len(asked[0]))
	}
	for _, r := range got.Resources {
		if (len(r.Metrics) == 1) != (r.Service == "service-3") {
			t.Fatalf("%s carries %+v", r.Service, r.Metrics)
		}
	}
}

// A fused read of everything on a trace of ten resources on three nodes asks Loki three times (the trace's lines, the
// context lines of all the resources, the system lines of all the nodes) and Prometheus once, where it asked Loki
// fourteen times and Prometheus twenty.
func TestAFusedReadOfEverythingAsksEachStoreForEachPartOnce(t *testing.T) {
	f := newFake(t)
	var res []testRes
	for i := 0; i < 10; i++ {
		res = append(res, testRes{"svc-" + strconv.Itoa(i), "shop", "pod-" + strconv.Itoa(i), "node-" + strconv.Itoa(i%3), "cl-1"})
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTraceOf(res...)) }
	ls := &lokiStore{}
	for i := 0; i < 10; i++ {
		ls.lines = append(ls.lines, appLines("svc-"+strconv.Itoa(i), "shop", "pod-"+strconv.Itoa(i), "cl-1", 10+i, 3, nil)...)
	}
	ls.lines = append(ls.lines, nodeLines("node-1", "cl-1", 50, 2, nil)...)
	f.loki = ls.handler(t)
	ps := &promStore{series: series("up", 2, lbl("service_name", "svc-4", "k8s_namespace_name", "shop", "ikhnos_cluster_id", "cl-1"))}
	f.prom = ps.handler(t)
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Logs: true, ContextLogs: true,
		SystemLogs: true, Metrics: true, Topology: true, Changes: true, Extras: &fakeExtras{view: testView()}})
	if err != nil {
		t.Fatal(err)
	}
	asked, _ := ps.reads()
	if len(ls.asked()) != 3 || len(asked) != 1 {
		t.Fatalf("Loki was asked %d times, Prometheus %d: %v %v", len(ls.asked()), len(asked), ls.asked(), asked)
	}
	for _, k := range []string{SignalLogs, SourceContextLogs, SourceSystemLogs, SignalMetrics, SourceTopology, SourceChanges} {
		if got.Sources[k] != SourceOK {
			t.Fatalf("sources %v, warnings %v", got.Sources, got.Warnings)
		}
	}
	if len(got.Resources[3].Logs) != 3 || len(got.Resources[4].Metrics) != 2 || len(got.SystemLogs.Entries) != 2 ||
		len(got.SystemLogs.Nodes) != 3 {
		t.Fatalf("resources %+v, system lines %+v", got.Resources, got.SystemLogs)
	}
}
