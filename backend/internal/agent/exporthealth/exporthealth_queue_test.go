package exporthealth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// What the real collector (0.160.0, default metric level) serves about an exporter's queue and about itself: the queue's size and capacity by
// kind of data, the items the queue refused, and the seconds it has been running.
const queueSample = `otelcol_exporter_queue_size{data_type="logs",exporter="otlp/logs"} 5
otelcol_exporter_queue_capacity{data_type="logs",exporter="otlp/logs"} 1000
otelcol_exporter_queue_capacity{data_type="metrics",exporter="idle"} 1000
otelcol_exporter_queue_capacity{data_type="logs",exporter="debug"} 1000
otelcol_exporter_enqueue_failed_log_records{exporter="otlp/logs"} 7
otelcol_exporter_enqueue_failed_log_records{exporter="debug"} 3
otelcol_exporter_sent_log_records{exporter="otlp/logs",server_address="a",server_port="4317"} 40
otelcol_process_uptime 412.5
`

func TestParseReadsTheQueueAndTheItemsItRefused(t *testing.T) {
	got, err := parse(strings.NewReader(queueSample))
	if err != nil {
		t.Fatal(err)
	}
	want := counts{sent: 40, queued: 5, capacity: 1000, dropped: 7}
	if c := got[route{"otlp/logs", "logs"}]; c != want {
		t.Errorf("otlp/logs = %+v, want %+v", c, want)
	}
	// A capacity says nothing without something else to say, and the debug exporter is nobody's destination.
	if len(got) != 1 {
		t.Errorf("got %+v, want only the route that has counters", got)
	}
}

func TestParseUptime(t *testing.T) {
	for name, c := range map[string]struct {
		body string
		want float64
		ok   bool
	}{
		"the collector's own":    {queueSample, 412.5, true},
		"with a seconds suffix":  {"otelcol_process_uptime_seconds 9\n", 9, true},
		"a similar name":         {"otelcol_process_uptime_foo 9\n", 0, false},
		"the source-health port": {"up{job=\"kepler\"} 1\n", 0, false},
		"not a number":           {"otelcol_process_uptime NaN\n", 0, false},
		"a negative number":      {"otelcol_process_uptime -4\n", 0, false},
	} {
		if got, ok := parseUptime(c.body); got != c.want || ok != c.ok {
			t.Errorf("%s: got %v, %v, want %v, %v", name, got, ok, c.want, c.ok)
		}
	}
}

func TestADroppedItemIsReportedAndDoesNotMakeAFailingRoute(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	ctx := context.Background()
	body := func(sent, queued, dropped int) string {
		return fmt.Sprintf("otelcol_exporter_sent_log_records{exporter=\"otlp\"} %d\notelcol_exporter_queue_size{data_type=\"logs\",exporter=\"otlp\"} %d\notelcol_exporter_queue_capacity{data_type=\"logs\",exporter=\"otlp\"} 100\notelcol_exporter_enqueue_failed_log_records{exporter=\"otlp\"} %d\n", sent, queued, dropped)
	}
	f.set("10.0.0.1:8888", body(500, 90, 0))
	m.Scrape(ctx)
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", body(520, 100, 12))
	m.Scrape(ctx)
	got := routeOf(t, m.Report(), "otlp", "logs")
	if got.State != ikhnosv1.ExportRouteHealth_EXPORTING {
		t.Errorf("state = %v, want exporting: it is still sending, and what it loses is said beside the state", got.State)
	}
	if got.Queued != 100 || got.QueueCapacity != 100 || got.Dropped != 12 || got.LastDroppedAt == nil {
		t.Errorf("queue = %d/%d, dropped %d at %v, want 100/100 and 12 with a time", got.Queued, got.QueueCapacity, got.Dropped, got.LastDroppedAt)
	}
	// Nothing more is refused, and the queue drains: the counters stay and the time of the last drop stays with them.
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", body(560, 0, 12))
	m.Scrape(ctx)
	got = routeOf(t, m.Report(), "otlp", "logs")
	if got.Queued != 0 || got.Dropped != 12 || got.LastDroppedAt == nil {
		t.Errorf("after the queue drained: %d queued, dropped %d at %v", got.Queued, got.Dropped, got.LastDroppedAt)
	}
}

// The cluster collector serves its counters on one port and its source health on another, so it is read twice and is one pod.
func TestAPodThatServesTwoPortsIsOnePod(t *testing.T) {
	f := newFleet()
	f.dns["cluster.svc"] = []string{"10.0.0.5"}
	ctx := context.Background()
	m := f.monitor("cluster.svc:8888", "cluster.svc:8889")

	f.set("10.0.0.5:8888", counter("otlp", "spans", 1, 0))
	f.set("10.0.0.5:8889", "up{job=\"kepler\"} 1\n")
	m.Scrape(ctx)
	if h := m.Report(); h.PodsReached != 1 || h.PodsFailed != 0 {
		t.Errorf("both ports read: reached %d, failed %d, want 1 and 0", h.PodsReached, h.PodsFailed)
	}

	// One port fails and the other answers: the pod is reached, the error is still said, and it is not a failed pod.
	delete(f.body, "10.0.0.5:8889")
	m.Scrape(ctx)
	if h := m.Report(); h.PodsReached != 1 || h.PodsFailed != 0 || h.LastError == "" {
		t.Errorf("one port failed: reached %d, failed %d, last error %q, want 1, 0 and the error", h.PodsReached, h.PodsFailed, h.LastError)
	}

	// Neither answers: one failed pod, not two.
	delete(f.body, "10.0.0.5:8888")
	m.Scrape(ctx)
	if h := m.Report(); h.PodsReached != 0 || h.PodsFailed != 1 {
		t.Errorf("no port answered: reached %d, failed %d, want 0 and 1", h.PodsReached, h.PodsFailed)
	}
}

func TestARouteNoCollectorReportsAnyMoreIsForgottenButNotTooSoon(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1"}
	m := f.monitor("host.svc:8888")
	m.cfg.Forget = 10 * time.Minute
	ctx := context.Background()
	both := counter("otlp/old", "log_records", 5, 0) + counter("otlp/new", "log_records", 5, 0)
	f.set("10.0.0.1:8888", both)
	m.Scrape(ctx)
	if len(m.Report().Routes) != 2 {
		t.Fatalf("routes = %+v, want both", m.Report().Routes)
	}
	// The old exporter is removed from the config: its series are gone.
	f.set("10.0.0.1:8888", counter("otlp/new", "log_records", 6, 0))
	f.advance(5 * time.Minute)
	m.Scrape(ctx)
	if len(m.Report().Routes) != 2 {
		t.Errorf("after 5 minutes: %+v, want it kept: a collector that has just restarted serves no series for a route until something goes through it", m.Report().Routes)
	}
	// While no collector can be read, nothing is said about what they report: nothing is forgotten.
	delete(f.body, "10.0.0.1:8888")
	f.advance(20 * time.Minute)
	m.Scrape(ctx)
	if len(m.Report().Routes) != 2 {
		t.Errorf("with no collector read: %+v, want both kept", m.Report().Routes)
	}
	f.set("10.0.0.1:8888", counter("otlp/new", "log_records", 7, 0))
	m.Scrape(ctx)
	r := m.Report().Routes
	if len(r) != 1 || r[0].Exporter != "otlp/new" {
		t.Errorf("after the collectors answered without it for over 10 minutes: %+v, want only otlp/new", r)
	}
}

func TestARestartOfACollectorIsCountedAndAReplacementIsNot(t *testing.T) {
	f := newFleet()
	f.dns["host.svc"] = []string{"10.0.0.1", "10.0.0.2"}
	m := f.monitor("host.svc:8888")
	ctx := context.Background()
	up := func(s int) string { return fmt.Sprintf("otelcol_process_uptime %d\n", s) }
	read := func() *ikhnosv1.ExportHealth { m.Scrape(ctx); return m.Report() }

	f.set("10.0.0.1:8888", up(1000))
	f.set("10.0.0.2:8888", up(50))
	if h := read(); h.CollectorRestarts != 0 {
		t.Fatalf("first reading: %d restarts, want 0 (nothing to compare with)", h.CollectorRestarts)
	}
	f.advance(30 * time.Second)
	f.set("10.0.0.1:8888", up(1030))
	f.set("10.0.0.2:8888", up(80))
	if h := read(); h.CollectorRestarts != 0 {
		t.Fatalf("both running on: %d restarts, want 0", h.CollectorRestarts)
	}
	// The second pod starts again at its own address.
	f.advance(30 * time.Second)
	f.set("10.0.0.2:8888", up(4))
	f.set("10.0.0.1:8888", up(1060))
	h := read()
	if h.CollectorRestarts != 1 || h.LastCollectorRestartAt == nil {
		t.Fatalf("after a restart: %d restarts at %v, want 1 with a time", h.CollectorRestarts, h.LastCollectorRestartAt)
	}
	// A pod that is replaced has a new address and a low uptime: it is not a restart.
	f.advance(30 * time.Second)
	f.dns["host.svc"] = []string{"10.0.0.1", "10.0.0.3"}
	delete(f.body, "10.0.0.2:8888")
	f.set("10.0.0.3:8888", up(2))
	f.set("10.0.0.1:8888", up(1090))
	if h := read(); h.CollectorRestarts != 1 {
		t.Fatalf("after a replacement: %d restarts, want still 1", h.CollectorRestarts)
	}
}
