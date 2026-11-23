package fusionapi

import (
	"context"
	"encoding/json"
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
	if !s.Unrestricted() || len(s.FocusServices) > 0 {
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
		seen := map[string]bool{}
		var out []string
		for _, h := range hits {
			for _, svc := range h.Services {
				// A trace that touches the focused services also names the ones it passed through; those are not the focus.
				if len(s.FocusServices) > 0 && !slices.Contains(s.FocusServices, svc) {
					continue
				}
				if !seen[svc] {
					seen[svc] = true
					out = append(out, svc)
				}
			}
		}
		sort.Strings(out)
		return out, sample, nil
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
	if len(s.Focus) > 0 && len(s.Clusters) == 0 {
		// An application's services, each where it runs: one read per place.
		return c.logServicesIn(ctx, s, tr)
	}
	if len(s.Clusters) > 0 {
		// A cluster the caller's own rights limit it to is matched after the selector (lines stored before the cluster became an
		// index label carry it as structured metadata only), and every line read is checked once more against the rights:
		// read the newest in-scope lines and take the services they came from. A focus's clusters are a choice, not a right,
		// and go the way below.
		lines, truncated, err := c.Logs(ctx, s, LogFilter{Backward: true}, tr.newest(MaxLogWindow), scopedLogSample)
		if err != nil {
			return nil, "", err
		}
		if truncated {
			sample = fmt.Sprintf("the newest %d log lines", scopedLogSample)
		}
		seen := map[string]bool{}
		var out []string
		for _, l := range lines {
			if l.Service != "" && !seen[l.Service] {
				seen[l.Service] = true
				out = append(out, l.Service)
			}
		}
		return out, sample, nil
	}
	sel, err := LogFilter{}.selector(s)
	if err != nil {
		return nil, "", err
	}
	names, err = c.lokiServiceNames(ctx, sel, s.FocusClusters, tr)
	return names, "", err
}

// logServicesIn is logServices for an application, place by place: the services of its names with lines in each of its
// namespaces and clusters. The places are asked about a few at a time, and the answer is sorted, so it reads the same
// whatever order they come back in.
func (c *Client) logServicesIn(ctx context.Context, s Scope, tr TimeRange) ([]string, string, error) {
	type result struct {
		names []string
		err   error
	}
	results := make([]result, len(s.Focus))
	sem := make(chan struct{}, logPlaceReads)
	var wg sync.WaitGroup
	for i, p := range s.Focus {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquire(ctx, sem)
			if err != nil {
				results[i].err = err
				return
			}
			defer release()
			// The caller's namespace rights are on every selector with the place's own namespace, as the other renderers put
			// rights and focus together: a place outside the rights reads nothing.
			sel := []string{lblService + "=~" + quote(regexAny(p.Names))}
			if p.Namespace != "" {
				sel = append(sel, lblNamespace+"="+quote(p.Namespace))
			}
			if len(s.Namespaces) > 0 {
				sel = append(sel, lblNamespace+"=~"+quote(regexAny(s.Namespaces)))
			}
			var cl []string
			if p.Cluster != "" {
				cl = []string{p.Cluster}
			}
			results[i].names, results[i].err = c.lokiServiceNames(ctx, "{"+strings.Join(sel, ",")+"}", cl, tr)
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if r.err != nil {
			// The first place's error in their order, not the first to fail, so the answer is the same each time.
			return nil, "", r.err
		}
		for _, n := range r.names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out, "", nil
}

// lokiServiceNames lists the services with lines in the range under a stream selector and, when clusters are given, in
// one of them. The index answers: the cluster goes into the stream selector here, because Loki's label-values call takes
// no pipeline. Lines stored before the cluster became an index label carry it as structured metadata only and are not
// counted; they leave with the store's retention. Reading the lines themselves to find them (a count over the range)
// would make a service list cost as much as reading every log line of the range.
func (c *Client) lokiServiceNames(ctx context.Context, sel string, clusters []string, tr TimeRange) ([]string, error) {
	if len(clusters) > 0 {
		cond := lblCluster + "=" + quote(clusters[0])
		if len(clusters) > 1 {
			cond = lblCluster + "=~" + quote(regexAny(clusters))
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
	sources := map[string]string{SignalMetrics: SourceNotAllowed, SignalLogs: SourceNotAllowed, SignalTraces: SourceNotAllowed}
	found := map[string]map[string]bool{}
	var warnings []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	run := func(signal string, list func() ([]string, string, error)) {
		if !s.Allows(signal) {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			names, sample, err := list()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				sources[signal] = sourceState(err)
				warnings = append(warnings, fmt.Sprintf("%s: %v", signal, err))
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			sources[signal] = SourceOK
			if sample != "" {
				warnings = append(warnings, fmt.Sprintf("%s: the services are those of %s; one that is not in them may be missing", signal, sample))
			}
			for _, n := range names {
				if found[n] == nil {
					found[n] = map[string]bool{}
				}
				found[n][signal] = true
			}
		}()
	}
	run(SignalMetrics, func() ([]string, string, error) { names, err := c.metricServices(ctx, s, tr); return names, "", err })
	run(SignalLogs, func() ([]string, string, error) { return c.logServices(ctx, s, tr) })
	run(SignalTraces, func() ([]string, string, error) { return c.traceServices(ctx, s, tr) })
	wg.Wait()
	if err := callerGone(ctx); err != nil {
		return nil, nil, nil, err
	}
	ok := false
	for _, v := range sources {
		ok = ok || v == SourceOK
	}
	if !ok && firstErr != nil {
		return nil, nil, nil, firstErr // nothing could be read at all
	}
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
	var sels []string
	if len(s.Focus) > 0 {
		// Each member's names where it runs: a member's name in another member's namespace is not the application's.
		wide := s
		wide.FocusServices = nil
		m, err := MetricFilter{}.matchers(wide)
		if err != nil {
			return nil, err
		}
		for _, c := range placeConds(s.Focus, lblNamespace, lblCluster, lblService, func(k, v string) string { return k + "=" + quote(v) },
			func(k string, vs []string) string { return k + "=~" + quote(regexAny(vs)) }) {
			sels = append(sels, "{"+strings.Join(append(slices.Clone(m), c...), ",")+"}")
		}
	} else {
		m, err := MetricFilter{}.matchers(s)
		if err != nil {
			return nil, err
		}
		sels = []string{"{" + strings.Join(append(m, lblService+`=~".+"`), ",") + "}"}
	}
	data, err := c.prom(ctx, "/api/v1/label/"+lblService+"/values", url.Values{"match[]": sels, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}})
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
	}
	return names, nil
}
