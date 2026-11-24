package fusionapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// traceServices lists the service names with spans in the range (inside the Scope). A Scope with a limit cannot ask
// Tempo for tag values restricted to it (the tag-values call does not promise to honour the query), so it reads the
// services off a sample of the traces it may see instead: the list is then those of the newest traces, not all, and sample
// says so (it is empty for a complete list).
func (c *Client) traceServices(ctx context.Context, s Scope, tr TimeRange) (names []string, sample string, err error) {
	if err := s.needSignal(SignalTraces); err != nil {
		return nil, "", err
	}
	focus := s.places().names
	if !s.Unrestricted() || len(focus) > 0 {
		hits, truncated, err := c.SearchTraces(ctx, s, TraceFilter{}, tr.newest(MaxSearchWindow), scopedServiceSample)
		if err != nil {
			return nil, "", err
		}
		if truncated {
			sample = fmt.Sprintf("the newest %d traces", scopedServiceSample)
		}
		if tr.To.Sub(tr.From) > MaxSearchWindow { // the search reads its newest part only: say so, not just when the sample was cut
			if sample == "" {
				sample = "the traces"
			}
			sample += fmt.Sprintf(" of the newest %s of the range", shortDuration(MaxSearchWindow))
		}
		var out []string
		for _, h := range hits {
			for _, svc := range h.Services {
				// A trace that touches the focused services also names the ones it passed through; those are not the focus.
				// (A hit names no service "", so within is the focus's names alone.)
				if within(focus, svc) {
					out = append(out, svc)
				}
			}
		}
		return distinctSorted(out), sample, nil
	}
	q := url.Values{"start": {unixSec(tr.From)}, "end": {unixSec(tr.To)}}
	var res struct {
		TagValues []struct {
			Value string `json:"value"`
		} `json:"tagValues"`
	}
	if err := c.get(ctx, storeTempo, c.Tempo, "/api/v2/search/tag/resource."+attrService+"/values", q, nil, &res); err != nil {
		return nil, "", err
	}
	var out []string
	for _, v := range res.TagValues {
		if v.Value != "" {
			out = append(out, v.Value)
		}
	}
	return out, "", nil
}

// scopedServiceSample is how many traces a limited Scope's service listing reads.
const scopedServiceSample = 200

// shortDuration writes a whole number of days or hours as 7d or 12h, and anything else as Go does.
func shortDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}

// scopedLogSample is how many lines a cluster-limited Scope's service listing reads.
const scopedLogSample = 1000

// logPlaceReads is how many of an application's places logServicesIn asks Loki about at once.
const logPlaceReads = 4

// logServices lists the service names that have logs in the range (inside the Scope). A Scope whose rights limit it to
// clusters reads them off a sample of the newest lines; sample says so (it is empty for a complete list).
func (c *Client) logServices(ctx context.Context, s Scope, tr TimeRange) (names []string, sample string, err error) {
	if err := s.needSignal(SignalLogs); err != nil {
		return nil, "", err
	}
	if places := logServicePlaces(s); len(places) > 0 {
		// An application's services, each where it runs: one read per place. For a caller limited to applications the places
		// are its rights' (or its focus's, inside them), and each names its cluster, which goes into the selector itself: a
		// line that carries its cluster in its structured metadata alone is not counted, which can only leave a name out.
		return c.logServicesIn(ctx, s, places, tr)
	}
	if len(s.Clusters) > 0 || s.AppLimited {
		// A cluster the caller's own rights limit it to is matched after the selector (lines stored before the cluster became an
		// index label carry it as structured metadata only), and every line read is checked once more against the rights:
		// read the newest in-scope lines and take the services they came from. A focus's clusters are a choice, not a right,
		// and go the way below. A caller limited to applications in more places than are worth a read each comes here too:
		// the lines are read with every place named, and each is checked against the places once more.
		lines, truncated, err := c.Logs(ctx, s, LogFilter{Backward: true}, tr.newest(MaxLogWindow), scopedLogSample)
		if err != nil {
			return nil, "", err
		}
		if truncated {
			sample = fmt.Sprintf("the newest %d log lines", scopedLogSample)
		}
		var out []string
		for _, l := range lines {
			if l.Service != "" {
				out = append(out, l.Service)
			}
		}
		return uniqueInOrder(out), sample, nil
	}
	sel, err := LogFilter{}.selector(s)
	if err != nil {
		return nil, "", err
	}
	// The rights limit no cluster here, so the places' clusters are the focus's alone.
	names, err = c.lokiServiceNames(ctx, sel, s.places().clusters, tr)
	return names, "", err
}

// logServicePlaces are the places logServices reads one by one (logServicesIn), none when it reads the lines another way.
func logServicePlaces(s Scope) []FocusPlace {
	p := s.places()
	if len(p.exact) > 0 && (len(s.Clusters) == 0 || s.AppLimited) && !(s.AppLimited && len(p.exact) > maxFocusPlaces) {
		return p.exact
	}
	return nil
}

// logServicesIn is logServices for an application, place by place: the services of its names with lines in each of its
// namespaces and clusters. The places are asked about a few at a time, and the answer is sorted, so it reads the same
// whatever order they come back in.
//
// One place that fails fails the list, so the first failure cancels the reads still running or waiting: their answers
// could not be used. The error returned is that first failure, recorded as it happens and before the others are
// cancelled, so it is never the "context canceled" this cancellation then causes in them (those come later and are
// dropped). When only one place fails, the answer is that place's error, whatever the timing.
func (c *Client) logServicesIn(ctx context.Context, s Scope, places []FocusPlace, tr TimeRange) ([]string, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu    sync.Mutex
		first error // the first failure; set once, and only then are the other reads cancelled
	)
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if first == nil {
			first = err
			cancel()
		}
	}
	results := make([][]string, len(places))
	sem := make(chan struct{}, logPlaceReads)
	var wg sync.WaitGroup
	for i, p := range places {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquire(ctx, sem)
			if err != nil {
				fail(err)
				return
			}
			defer release()
			// The caller's namespace rights are on every selector with the place's own namespace, as the other renderers put
			// rights and focus together: a place outside the rights reads nothing. Unlike placeSet.placeConds, the names
			// come first, the rights' namespaces follow (not the focus's union: the place names its own), and the cluster is
			// left to lokiServiceNames, because Loki's label-values call takes a stream selector alone.
			d := labelSyntax
			sel := []string{d.in(d.name, p.Names)}
			if p.Namespace != "" {
				sel = append(sel, d.eq(d.namespace, p.Namespace))
			}
			if rightsNs, _, _ := s.rights().cutOn(d); rightsNs != "" {
				sel = append(sel, rightsNs)
			}
			var cl []string
			if p.Cluster != "" {
				cl = []string{p.Cluster}
			}
			names, err := c.lokiServiceNames(ctx, "{"+strings.Join(sel, ",")+"}", cl, tr)
			if err != nil {
				fail(err)
				return
			}
			results[i] = names
		}()
	}
	wg.Wait()
	if first != nil { // (every goroutine is done: wg.Wait orders their writes before this read)
		return nil, "", first
	}
	return distinctSorted(slices.Concat(results...)), "", nil
}

// lokiServiceNames lists the services with lines in the range under a stream selector and, when clusters are given, in
// one of them. The index answers: the cluster goes into the stream selector here, because Loki's label-values call takes
// no pipeline. Lines stored before the cluster became an index label carry it as structured metadata only and are not
// counted; they leave with the store's retention. Reading the lines themselves to find them (a count over the range)
// would make a service list cost as much as reading every log line of the range.
func (c *Client) lokiServiceNames(ctx context.Context, sel string, clusters []string, tr TimeRange) ([]string, error) {
	if len(clusters) > 0 {
		d := labelSyntax
		cond := d.eq(d.cluster, clusters[0])
		if len(clusters) > 1 {
			cond = d.in(d.cluster, clusters)
		}
		sel = strings.TrimSuffix(sel, "}") + "," + cond + "}"
	}
	var res struct {
		Data []string `json:"data"`
	}
	if err := c.get(ctx, storeLoki, c.Loki, "/loki/api/v1/label/"+lblService+"/values", url.Values{
		"query": {sel}, "start": {unixNano(tr.From)}, "end": {unixNano(tr.To)},
	}, nil, &res); err != nil {
		return nil, err
	}
	if res.Data == nil {
		return nil, errf(http.StatusBadGateway, "%s answered with something unexpected", storeLoki)
	}
	return res.Data, nil
}

// Application is a service FUSION has data for, and which signal types.
type Application struct {
	Name    string   `json:"name"`
	Signals []Signal `json:"signals"`
}

// Applications lists the services that have telemetry in the range, each with the signal types it has. Signals the
// Scope does not include are not looked at; a store that cannot be reached is reported in the returned sources, and the
// warnings say why a part failed and which lists were built from a sample (and so may miss services).
func (c *Client) Applications(ctx context.Context, s Scope, tr TimeRange) ([]Application, map[string]string, []string, error) {
	reads := newSignalReads(s)
	found := map[string]map[string]bool{}
	run := func(signal string, list func() ([]string, string, error)) {
		reads.run(signal, func() (func(), error) {
			names, sample, err := list()
			if err != nil {
				return nil, err
			}
			return func() {
				if sample != "" {
					reads.warnings = append(reads.warnings, fmt.Sprintf(
						"%s: the services are those of %s; one that is not in them may be missing", signal, sample))
				}
				for _, n := range names {
					if found[n] == nil {
						found[n] = map[string]bool{}
					}
					found[n][signal] = true
				}
			}, nil
		})
	}
	run(SignalMetrics, func() ([]string, string, error) { names, err := c.metricServices(ctx, s, tr); return names, "", err })
	run(SignalLogs, func() ([]string, string, error) { return c.logServices(ctx, s, tr) })
	run(SignalTraces, func() ([]string, string, error) { return c.traceServices(ctx, s, tr) })
	if err := reads.wait(ctx); err != nil {
		return nil, nil, nil, err // the caller has gone, or nothing could be read at all
	}
	sources, warnings := reads.sources, reads.warnings
	var apps []Application
	for name, sigs := range found {
		a := Application{Name: name}
		for _, sig := range Signals {
			if sigs[sig] {
				a.Signals = append(a.Signals, Signal(sig))
			}
		}
		apps = append(apps, a)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	sort.Strings(warnings) // (the reads finish in any order)
	return apps, sources, warnings, nil
}

// metricServices lists the service names that have series in the range (inside the Scope).
func (c *Client) metricServices(ctx context.Context, s Scope, tr TimeRange) ([]string, error) {
	q, err := metricServicesQuery(s, tr)
	if err != nil {
		return nil, err
	}
	return c.promLabelValues(ctx, lblService, q)
}

// metricServicesQuery is the label-values question metricServices asks: its selectors and range.
func metricServicesQuery(s Scope, tr TimeRange) (url.Values, error) {
	var sels []string
	if p := s.places(); len(p.exact) > 0 {
		// Each member's names where it runs: a member's name in another member's namespace is not the application's.
		m, err := MetricFilter{}.matchersIn(p.unnamed())
		if err != nil {
			return nil, err
		}
		sels = p.promSelectors(m, []string{lblService})
	} else {
		// (With a focus on names alone, the names are in m already; the matcher that cannot be empty is added all the same.)
		m, err := MetricFilter{}.matchers(s)
		if err != nil {
			return nil, err
		}
		sels = []string{"{" + strings.Join(append(m, lblService+`=~".+"`), ",") + "}"}
	}
	return url.Values{"match[]": sels, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}}, nil
}

// ServiceListReads is how many store reads listing the services (Applications) makes for a caller with Scope s over tr,
// of the signals it may read: Prometheus once per batch of selectors (promLabelValues: one, unless a caller limited to
// applications has more places than one question holds), Loki once per place when an application's services are read
// place by place (logServicesIn) and once otherwise, and Tempo once. The server charges a caller's rate limit by it.
func ServiceListReads(s Scope, tr TimeRange) int {
	n := 0
	if s.Allows(SignalMetrics) {
		if q, err := metricServicesQuery(s, tr); err == nil { // (a query that cannot be built is not asked)
			_, batches := labelValueBatches(q)
			n += len(batches)
		}
	}
	if s.Allows(SignalLogs) {
		n += max(len(logServicePlaces(s)), 1)
	}
	if s.Allows(SignalTraces) {
		n++
	}
	return max(n, 1)
}
