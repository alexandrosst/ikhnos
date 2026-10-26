package exporthealth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// The counters as the real collector (0.160.0) serves them: no _total suffix, the exporter id as the label,
// and the address an OTLP exporter talks to as extra labels.
const sample = `# HELP otelcol_exporter_sent_metric_points Number of metric points successfully sent to destination.
# TYPE otelcol_exporter_sent_metric_points counter
otelcol_exporter_sent_metric_points{exporter="otlphttp/metrics",otel_scope_name="x",server_address="127.0.0.1",server_port="9999",url_path="/v1/metrics"} 126
otelcol_exporter_send_failed_metric_points{exporter="otlphttp/bad",server_address="127.0.0.1",server_port="9998",url_path="/v1/metrics"} 7
otelcol_exporter_sent_log_records{exporter="otlphttp/logs"} 1
otelcol_exporter_sent_spans{exporter="zipkin/traces"} 3
otelcol_exporter_sent_spans{exporter="debug"} 99
otelcol_exporter_queue_size{exporter="otlp",data_type="logs"} 0
otelcol_exporter_in_flight_requests{exporter="otlp"} 0
otelcol_processor_batch_batch_send_size_sum{processor="batch"} 12
`

func TestParseReadsExportCountersOnly(t *testing.T) {
	got, err := parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	want := map[route]counts{
		{"otlphttp/metrics", "metrics"}: {sent: 126},
		{"otlphttp/bad", "metrics"}:     {failed: 7},
		{"otlphttp/logs", "logs"}:       {sent: 1},
		{"zipkin/traces", "traces"}:     {sent: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v = %+v, want %+v", k, got[k], v)
		}
	}
}

func TestParseAcceptsTotalSuffixSumsSeriesAndHandlesAwkwardLabels(t *testing.T) {
	got, err := parse(strings.NewReader(`otelcol_exporter_sent_spans_total{exporter="otlp",server_address="a",url_path="/v1/{x},\"y\""} 5 1700000000000
otelcol_exporter_sent_spans_total{exporter="otlp",server_address="b"} 7
otelcol_exporter_send_failed_spans_total{exporter="otlp"} 1.0
otelcol_exporter_sent_spans{exporter="otlp",bad
otelcol_exporter_sent_spans{exporter="otlp"} NaN
otelcol_exporter_sent_spans{exporter="otlp"} -3
otelcol_exporter_sent_spans{exporter=""} 4
`))
	if err != nil {
		t.Fatal(err)
	}
	if c := got[route{"otlp", "traces"}]; c.sent != 12 || c.failed != 1 || len(got) != 1 {
		t.Errorf("got %+v, want one route with 12 sent and 1 failed", got)
	}
}

// fleet is a set of fake collector pods whose counters a test moves.
type fleet struct {
	mu   sync.Mutex
	dns  map[string][]string // name -> addresses
	body map[string]string   // address -> metrics text
	err  map[string]error    // address -> failure
	now  time.Time
}

func newFleet() *fleet {
	return &fleet{dns: map[string][]string{}, body: map[string]string{}, err: map[string]error{}, now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
}

func (f *fleet) monitor(targets ...string) *Monitor {
	return New(Config{
		Targets: targets, Window: 3 * time.Minute,
		Now: func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
		Lookup: func(_ context.Context, host string) ([]string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			ips, ok := f.dns[host]
			if !ok {
				return nil, fmt.Errorf("no such host")
			}
			return ips, nil
		},
		Get: func(_ context.Context, url string) ([]byte, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			addr := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/metrics")
			if err := f.err[addr]; err != nil {
				return nil, err
			}
			b, ok := f.body[addr]
			if !ok {
				return nil, fmt.Errorf("connection refused")
			}
			return []byte(b), nil
		},
	})
}

func (f *fleet) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }
func (f *fleet) set(addr, text string)   { f.mu.Lock(); f.body[addr] = text; f.mu.Unlock() }

func counter(exporter, signal string, sent, failed int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "otelcol_exporter_sent_%s{exporter=%q} %d\n", signal, exporter, sent)
	fmt.Fprintf(&b, "otelcol_exporter_send_failed_%s{exporter=%q} %d\n", signal, exporter, failed)
	return b.String()
}

func routeOf(t *testing.T, h *ikhnosv1.ExportHealth, exporter, signal string) *ikhnosv1.ExportRouteHealth {
	t.Helper()
	if h == nil {
		t.Fatal("no report")
	}
	for _, r := range h.Routes {
		if r.Exporter == exporter && r.Signal == signal {
			return r
		}
	}
	t.Fatalf("no route %s/%s in %+v", exporter, signal, h.Routes)
	return nil
}

func TestStateFollowsGrowthNotCounterValues(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	ctx := context.Background()
	if m.Report() != nil {
		t.Fatal("a report before any reading")
	}

	// The first reading of a pod is a baseline: counters already above zero say nothing about now.
	f.set("10.0.0.1:8888", counter("otlp", "log_records", 500, 0))
	m.Scrape(ctx)
	if got := routeOf(t, m.Report(), "otlp", "logs"); got.State != ikhnosv1.ExportRouteHealth_WAITING || got.Sent != 500 {
		t.Fatalf("after the first reading: %+v, want waiting with 500 sent", got)
	}

	// It grows: exporting.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", counter("otlp", "log_records", 520, 0))
	m.Scrape(ctx)
	if got := routeOf(t, m.Report(), "otlp", "logs"); got.State != ikhnosv1.ExportRouteHealth_EXPORTING || got.LastSentAt == nil {
		t.Fatalf("after growth: %+v, want exporting", got)
	}

	// It stops, and stays stopped past the window: silent, never an error by itself.
	for i := 0; i < 8; i++ {
		f.advance(30 * time.Second)
		m.Scrape(ctx)
	}
	if got := routeOf(t, m.Report(), "otlp", "logs"); got.State != ikhnosv1.ExportRouteHealth_SILENT {
		t.Fatalf("after 4 minutes without growth: %+v, want silent", got)
	}

	// Sends start failing: failing, with the count.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", counter("otlp", "log_records", 520, 4))
	m.Scrape(ctx)
	got := routeOf(t, m.Report(), "otlp", "logs")
	if got.State != ikhnosv1.ExportRouteHealth_FAILING || got.Failed != 4 || got.LastFailedAt == nil {
		t.Fatalf("after failures: %+v, want failing", got)
	}

	// A send gets through again after the failures: exporting again, even though the failures stay counted.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", counter("otlp", "log_records", 530, 4))
	m.Scrape(ctx)
	if got := routeOf(t, m.Report(), "otlp", "logs"); got.State != ikhnosv1.ExportRouteHealth_EXPORTING {
		t.Fatalf("after recovery: %+v, want exporting", got)
	}
}

func TestFailuresWithNothingEverSentAreFailing(t *testing.T) {
	f := newFleet()
	f.dns["c.svc"] = []string{"10.0.0.9"}
	m := f.monitor("c.svc:8888")
	f.set("10.0.0.9:8888", "")
	m.Scrape(context.Background())
	// Nothing has been counted yet at all: waiting, not failing.
	if h := m.Report(); len(h.Routes) != 0 || h.PodsReached != 1 {
		t.Fatalf("report = %+v", h)
	}
	f.advance(30 * time.Second)
	f.set("10.0.0.9:8888", counter("zipkin/traces", "spans", 0, 2))
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "zipkin/traces", "traces"); got.State != ikhnosv1.ExportRouteHealth_FAILING {
		t.Fatalf("got %+v, want failing", got)
	}
}

func TestRestartedPodCountsFromZeroAndIsNotMistakenForSilence(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	f.set("10.0.0.1:8888", counter("otlp", "metric_points", 9000, 0))
	m.Scrape(context.Background())
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", counter("otlp", "metric_points", 40, 0)) // the collector restarted
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.State != ikhnosv1.ExportRouteHealth_EXPORTING {
		t.Fatalf("got %+v, want exporting after a counter reset", got)
	}
}

func TestSumsOverPodsAndSurvivesOneUnreachablePod(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1", "10.0.0.2"}
	m := f.monitor("host.svc:8888")
	f.set("10.0.0.1:8888", counter("otlp", "metric_points", 10, 0))
	f.set("10.0.0.2:8888", counter("otlp", "metric_points", 20, 0))
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.Sent != 30 {
		t.Fatalf("sent = %d, want 30 summed over both pods", got.Sent)
	}

	// One pod stops answering: it is counted and named, and the other still tells.
	f.advance(30 * time.Second)
	f.mu.Lock()
	f.err["10.0.0.2:8888"] = fmt.Errorf("connection reset")
	f.mu.Unlock()
	f.set("10.0.0.1:8888", counter("otlp", "metric_points", 15, 0))
	m.Scrape(context.Background())
	h := m.Report()
	if h.PodsReached != 1 || h.PodsFailed != 1 || !strings.Contains(h.LastError, "10.0.0.2") {
		t.Fatalf("report = %+v", h)
	}
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.State != ikhnosv1.ExportRouteHealth_EXPORTING {
		t.Fatalf("got %+v, want exporting from the pod that answered", got)
	}

	// The second pod comes back with the same counter: not new growth, and no bogus jump from a lost baseline.
	f.advance(30 * time.Second)
	f.mu.Lock()
	delete(f.err, "10.0.0.2:8888")
	f.mu.Unlock()
	m.Scrape(context.Background())
	if h := m.Report(); h.PodsFailed != 0 || h.PodsReached != 2 {
		t.Fatalf("report = %+v", h)
	}
}

func TestUnreadableCollectorsAreReportedNotMistakenForSilence(t *testing.T) {
	f := newFleet()
	m := f.monitor("gone.svc:8888", "bad-target")
	m.Scrape(context.Background())
	h := m.Report()
	if h == nil || h.PodsReached != 0 || h.PodsFailed != 2 || h.LastError == "" || len(h.Routes) != 0 {
		t.Fatalf("report = %+v, want nothing reached and the reasons said", h)
	}
}

func TestParseTargets(t *testing.T) {
	got := ParseTargets(" a:1, b:2 ,,a:1")
	if len(got) != 2 || got[0] != "a:1" || got[1] != "b:2" {
		t.Errorf("got %v", got)
	}
	if ParseTargets("") != nil {
		t.Error("no targets must be nil")
	}
}

func receiverLine(receiver, kind string, n int) string {
	return fmt.Sprintf("otelcol_receiver_%s{receiver=%q,transport=\"grpc\"} %d\n", kind, receiver, n)
}

func receiverOf(t *testing.T, h *ikhnosv1.ExportHealth, signal string) *ikhnosv1.ReceiverHealth {
	t.Helper()
	if h == nil {
		t.Fatal("no report")
	}
	for _, r := range h.Receivers {
		if r.Signal == signal {
			return r
		}
	}
	return nil
}

// What the applications push to the collector is counted apart from what the collector sends on, so a signal that never arrives is told
// as that and not as a destination that is quiet.
func TestWhatTheOTLPReceiverTookInIsReportedApartFromTheExporters(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	ctx := context.Background()

	// No span has ever arrived: there is no series at all, and the report has no entry for traces (the page says "nothing yet").
	f.set("10.0.0.1:8888", counter("otlp", "spans", 0, 0)+receiverLine("otlp", "accepted_log_records", 40)+
		receiverLine("k8s_cluster", "accepted_metric_points", 900)+receiverLine("prometheus/infra", "accepted_metric_points", 5))
	m.Scrape(ctx)
	h := m.Report()
	if receiverOf(t, h, "traces") != nil {
		t.Fatalf("traces have no receiver series: %+v", h.Receivers)
	}
	if receiverOf(t, h, "metrics") != nil {
		t.Fatalf("a receiver that is not OTLP was counted: %+v", h.Receivers)
	}
	for _, r := range h.Routes {
		if strings.HasPrefix(r.Exporter, "receiver") {
			t.Fatalf("a receiver is listed as a destination: %+v", r)
		}
	}

	// The logs have a baseline now; they grow: receiving.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", counter("otlp", "spans", 0, 0)+receiverLine("otlp", "accepted_log_records", 70))
	m.Scrape(ctx)
	if r := receiverOf(t, m.Report(), "logs"); r == nil || r.State != ikhnosv1.ReceiverHealth_RECEIVING || r.Accepted != 70 || r.LastAcceptedAt == nil {
		t.Fatalf("logs after growth: %+v", r)
	}

	// The first span arrives (a series that is new counts from zero): receiving.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", receiverLine("otlp", "accepted_log_records", 70)+receiverLine("otlp", "accepted_spans", 12))
	m.Scrape(ctx)
	if r := receiverOf(t, m.Report(), "traces"); r == nil || r.State != ikhnosv1.ReceiverHealth_RECEIVING || r.Accepted != 12 {
		t.Fatalf("traces after the first span: %+v", r)
	}

	// Nothing more for longer than the window: silent, not an error.
	for i := 0; i < 8; i++ {
		f.advance(30 * time.Second)
		m.Scrape(ctx)
	}
	if r := receiverOf(t, m.Report(), "traces"); r.State != ikhnosv1.ReceiverHealth_SILENT {
		t.Fatalf("traces after four quiet minutes: %+v", r)
	}

	// The receiver turns data away: refusing.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", receiverLine("otlp", "accepted_log_records", 70)+receiverLine("otlp", "accepted_spans", 12)+receiverLine("otlp", "refused_spans", 9))
	m.Scrape(ctx)
	if r := receiverOf(t, m.Report(), "traces"); r.State != ikhnosv1.ReceiverHealth_REFUSING || r.Refused != 9 {
		t.Fatalf("traces after refusals: %+v", r)
	}
}

func sourceOf(t *testing.T, h *ikhnosv1.ExportHealth, receiver string) *ikhnosv1.SourceHealth {
	t.Helper()
	if h == nil {
		t.Fatal("no report")
	}
	for _, s := range h.Sources {
		if s.Receiver == receiver {
			return s
		}
	}
	return nil
}

func scraperErrors(receiver, scraper string, n int) string {
	return fmt.Sprintf("otelcol_scraper_errored_metric_points_total{receiver=%q,scraper=%q} %d\n", receiver, scraper, n)
}

// The receivers that read a source of the cluster tell what they took in and what they failed to read, each for the signals it serves, and
// are never mistaken for the OTLP receiver or for a destination.
func TestTheReceiversThatReadTheClusterTellWhereASourceGoesSilentOrFails(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	ctx := context.Background()
	body := func(kubelet, kubeletErr, host, hostErr, events int) string {
		return receiverLine("kubelet_stats", "accepted_metric_points", kubelet) + scraperErrors("kubelet_stats", "node", kubeletErr) +
			receiverLine("hostmetrics", "accepted_metric_points", host) + scraperErrors("hostmetrics", "process", hostErr) +
			receiverLine("k8sobjects", "accepted_log_records", events) +
			receiverLine("prometheus/infra", "accepted_metric_points", 5) + counter("otlp", "metrics", 1, 0)
	}

	// The first reading is only a baseline: nothing is claimed.
	f.set("10.0.0.1:8888", body(100, 0, 500, 0, 7))
	m.Scrape(ctx)
	h := m.Report()
	if s := sourceOf(t, h, "kubelet_stats"); s == nil || s.State != ikhnosv1.SourceHealth_WAITING {
		t.Fatalf("after the first reading: %+v", s)
	}
	// Only the receivers that read the cluster are here; a Prometheus scrape is not, and neither is a destination.
	if len(h.Sources) != 3 || sourceOf(t, h, "prometheus/infra") != nil {
		t.Fatalf("sources = %+v", h.Sources)
	}
	if k := sourceOf(t, h, "kubelet_stats"); len(k.Signals) != 2 || k.Signals[0] != "resourceUsage" || k.Signals[1] != "nodeRuntime" {
		t.Fatalf("the kubelet serves two signals: %+v", k.Signals)
	}

	// Everything grows: receiving.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", body(160, 0, 900, 0, 9))
	m.Scrape(ctx)
	h = m.Report()
	for _, r := range []string{"kubelet_stats", "hostmetrics", "k8sobjects"} {
		if s := sourceOf(t, h, r); s.State != ikhnosv1.SourceHealth_RECEIVING || s.LastAcceptedAt == nil {
			t.Fatalf("%s after growth: %+v", r, s)
		}
	}

	// The kubelet stops answering (its certificate is refused: the scraper errors and nothing comes in): failing. The host reads most things and
	// fails one (a process it may not see): degraded. The events stay quiet: nothing is wrong yet.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", body(160, 12, 1300, 40, 9))
	m.Scrape(ctx)
	h = m.Report()
	if s := sourceOf(t, h, "kubelet_stats"); s.State != ikhnosv1.SourceHealth_FAILING || s.Failed != 12 || s.LastFailedAt == nil {
		t.Fatalf("kubelet failing: %+v", s)
	}
	if s := sourceOf(t, h, "hostmetrics"); s.State != ikhnosv1.SourceHealth_DEGRADED {
		t.Fatalf("host degraded: %+v", s)
	}
	if s := sourceOf(t, h, "k8sobjects"); s.State != ikhnosv1.SourceHealth_RECEIVING {
		t.Fatalf("events within the window: %+v", s)
	}

	// Quiet for longer than the window: the events are silent, which is not an error; the failures are no longer recent.
	for i := 0; i < 8; i++ {
		f.advance(30 * time.Second)
		m.Scrape(ctx)
	}
	h = m.Report()
	if s := sourceOf(t, h, "k8sobjects"); s.State != ikhnosv1.SourceHealth_SILENT {
		t.Fatalf("events after four quiet minutes: %+v", s)
	}
	if s := sourceOf(t, h, "kubelet_stats"); s.State != ikhnosv1.SourceHealth_SILENT {
		t.Fatalf("a kubelet that stopped failing and brings nothing is silent: %+v", s)
	}
}

// What the cluster collector serves on its source-health port, as the real collector wrote it (otelcol-contrib 0.160.0, see the chart's
// TestSourceHealthInTheRealCollector): the scrape's own series of each target, labelled with the job, the instance and the node.
const toolsFixture = `# HELP scrape_samples_scraped The number of samples the target exposed
# TYPE scrape_samples_scraped gauge
scrape_samples_scraped{instance="10.0.0.5:9102",job="kepler",k8s_node_name="node-b",node="node-b",otel_scope_name="github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver",otel_scope_schema_url="",otel_scope_version="0.160.0",server_port="9102",service_instance_id="10.0.0.5:9102",service_name="kepler",url_scheme="http"} 0
scrape_samples_scraped{instance="10.0.0.4:9102",job="kepler",node="node-a",service_name="kepler"} 412
scrape_samples_scraped{instance="10.0.0.6:9102",job="kepler",node="node-c",service_name="kepler"} 398
scrape_samples_scraped{instance="10.0.1.1:9400",job="dcgm-exporter",node="gpu-1",service_name="dcgm-exporter"} 60
scrape_samples_scraped{instance="app:9090",job="my-own-app",service_name="my-own-app"} 5
# HELP target_info Target metadata
# TYPE target_info gauge
target_info{instance="10.0.0.5:9102",job="kepler"} 1
# HELP up The scraping was successful
# TYPE up gauge
up{instance="10.0.0.5:9102",job="kepler",k8s_node_name="node-b",node="node-b",otel_scope_name="x",service_name="kepler"} 0
up{instance="10.0.0.4:9102",job="kepler",node="node-a",service_name="kepler"} 1
up{instance="10.0.0.6:9102",job="kepler",node="node-c",service_name="kepler"} 1
up{instance="10.0.1.1:9400",job="dcgm-exporter",node="gpu-1",service_name="dcgm-exporter"} 1
up{instance="app:9090",job="my-own-app",service_name="my-own-app"} 1
`

func toolOf(t *testing.T, h *ikhnosv1.ExportHealth, job string) *ikhnosv1.SourceHealth {
	t.Helper()
	if h == nil {
		t.Fatal("no report")
	}
	for _, s := range h.Sources {
		if s.Receiver == job {
			return s
		}
	}
	return nil
}

// The tools the collector scrapes are told by whether each target answered with series: all of them is receiving, some is degraded and names the
// nodes that did not, none is failing; a target that answers with nothing does not count; and a job that is not a tool is not reported.
func TestTheToolsTheCollectorScrapesAreToldByWhetherEachTargetAnswered(t *testing.T) {
	f := newFleet()
	f.dns["cluster.svc"] = []string{"10.0.0.9"}
	m := f.monitor("cluster.svc:8889")
	f.set("10.0.0.9:8889", toolsFixture)
	m.Scrape(context.Background())
	h := m.Report()

	k := toolOf(t, h, "kepler")
	if k == nil || k.State != ikhnosv1.SourceHealth_DEGRADED || k.Targets != 3 || k.TargetsUp != 2 || len(k.Down) != 1 || k.Down[0] != "node-b" {
		t.Fatalf("kepler = %+v, want degraded, 2 of 3 answering, node-b down", k)
	}
	if len(k.Signals) != 1 || k.Signals[0] != "energy" {
		t.Fatalf("kepler serves the energy signal: %v", k.Signals)
	}
	d := toolOf(t, h, "dcgm-exporter")
	if d == nil || d.State != ikhnosv1.SourceHealth_RECEIVING || d.Targets != 1 || d.TargetsUp != 1 || len(d.Down) != 0 || d.Signals[0] != "accelerators" {
		t.Fatalf("dcgm = %+v, want receiving on its one node", d)
	}
	if toolOf(t, h, "my-own-app") != nil {
		t.Fatalf("a scrape target of the owner's application is not a tool: %+v", h.Sources)
	}

	// No target answers: failing. One that answers with no series does not count as answering.
	f.set("10.0.0.9:8889", strings.NewReplacer(`node="node-a",service_name="kepler"} 1`, `node="node-a",service_name="kepler"} 0`, `node="node-c",service_name="kepler"} 1`, `node="node-c",service_name="kepler"} 1`,
		`job="kepler",node="node-c",service_name="kepler"} 398`, `job="kepler",node="node-c",service_name="kepler"} 0`).Replace(toolsFixture))
	m.Scrape(context.Background())
	k = toolOf(t, m.Report(), "kepler")
	if k.State != ikhnosv1.SourceHealth_FAILING || k.TargetsUp != 0 || len(k.Down) != 3 {
		t.Fatalf("kepler with nothing answering with series = %+v, want failing, all three down", k)
	}

	// The endpoint cannot be read at all: nothing is claimed about the tools (the report says so by having no entry), not that they are down.
	f.set("10.0.0.9:8889", "")
	delete(f.body, "10.0.0.9:8889")
	m.Scrape(context.Background())
	if toolOf(t, m.Report(), "kepler") != nil {
		t.Fatalf("an unreadable endpoint was reported as tools: %+v", m.Report().Sources)
	}
}

// The agent's own link service answers with no series until a link is measured: that is waiting, where Kepler answering with nothing is failing.
func TestTheLinkServiceThatIsUpWithNothingToReportIsWaitingNotFailing(t *testing.T) {
	f := newFleet()
	f.dns["cluster.svc"] = []string{"10.0.0.9"}
	m := f.monitor("cluster.svc:8889")
	body := `scrape_samples_scraped{instance="agent:9464",job="cluster-links",service_name="cluster-links"} 0
up{instance="agent:9464",job="cluster-links",service_name="cluster-links"} 1
scrape_samples_scraped{instance="10.0.0.4:9102",job="kepler",node="node-a",service_name="kepler"} 0
up{instance="10.0.0.4:9102",job="kepler",node="node-a",service_name="kepler"} 1
`
	f.set("10.0.0.9:8889", body)
	m.Scrape(context.Background())
	h := m.Report()
	if l := toolOf(t, h, "cluster-links"); l == nil || l.State != ikhnosv1.SourceHealth_WAITING || len(l.Down) != 0 {
		t.Fatalf("cluster-links up with no series = %+v, want waiting", l)
	}
	if k := toolOf(t, h, "kepler"); k == nil || k.State != ikhnosv1.SourceHealth_FAILING {
		t.Fatalf("kepler up with no series = %+v, want failing", k)
	}
	// Once a link is measured it is receiving.
	f.set("10.0.0.9:8889", strings.Replace(body, `service_name="cluster-links"} 0`, `service_name="cluster-links"} 12`, 1))
	m.Scrape(context.Background())
	if l := toolOf(t, m.Report(), "cluster-links"); l.State != ikhnosv1.SourceHealth_RECEIVING {
		t.Fatalf("cluster-links with series = %+v, want receiving", l)
	}
}

func queue(exporter, signal string, items int) string {
	return fmt.Sprintf("otelcol_exporter_queue_size{exporter=%q,data_type=%q} %d\n", exporter, signal, items)
}

// A destination that cannot be reached is retried for a long time before a send is counted as failed, so the counters stay still. A queue that
// holds data at two readings in a row while nothing is sent in between is what shows it.
func TestAQueueThatDoesNotEmptyIsAnExporterThatIsNotSending(t *testing.T) {
	f := newFleet()
	f.dns["c.svc"] = []string{"10.0.0.9"}
	m := f.monitor("c.svc:8888")
	f.set("10.0.0.9:8888", counter("otlp", "metric_points", 0, 0)+queue("otlp", "metrics", 12))
	m.Scrape(context.Background())
	f.advance(30 * time.Second)
	f.set("10.0.0.9:8888", counter("otlp", "metric_points", 0, 0)+queue("otlp", "metrics", 40))
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.State != ikhnosv1.ExportRouteHealth_FAILING {
		t.Fatalf("data waiting at two readings with nothing sent: got %+v, want failing", got)
	}

	// A queue that has something in it once, between two sends, is a collector working: sending resumes and the route is exporting.
	f.advance(30 * time.Second)
	f.set("10.0.0.9:8888", counter("otlp", "metric_points", 55, 0)+queue("otlp", "metrics", 3))
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.State != ikhnosv1.ExportRouteHealth_EXPORTING {
		t.Fatalf("a queue that is being drained: got %+v, want exporting", got)
	}
}

func TestAQueueThatHoldsDataOnceAndIsNotSentFromIsNotAFailure(t *testing.T) {
	f := newFleet()
	f.dns["c.svc"] = []string{"10.0.0.9"}
	m := f.monitor("c.svc:8888")
	f.set("10.0.0.9:8888", counter("otlp", "metric_points", 100, 0)+queue("otlp", "metrics", 0))
	m.Scrape(context.Background())
	f.advance(30 * time.Second)
	f.set("10.0.0.9:8888", counter("otlp", "metric_points", 100, 0)+queue("otlp", "metrics", 5))
	m.Scrape(context.Background())
	if got := routeOf(t, m.Report(), "otlp", "metrics"); got.State == ikhnosv1.ExportRouteHealth_FAILING {
		t.Fatalf("one reading of a queue with data in it is not a failure: %+v", got)
	}
}
