package fusionapi

// The lists and overviews that are not about one trace: the services with telemetry, and one service or application at a glance.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

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
	m, err := MetricFilter{}.matchers(s)
	if err != nil {
		return nil, err
	}
	m = append(m, lblService+`=~".+"`)
	sel := "{" + strings.Join(m, ",") + "}"
	data, err := c.prom(ctx, "/api/v1/label/"+lblService+"/values", url.Values{"match[]": {sel}, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}})
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
	}
	return names, nil
}

// Overview is one application at a glance: which signals it has and a few of the most recent and most interesting
// things in each, ready to follow into a fused trace.
type Overview struct {
	// ID and Services are set for an ikhnos application: its id and the service names its telemetry may carry.
	ID          string         `json:"id,omitempty"`
	Services    []string       `json:"services,omitempty"`
	Name        string         `json:"name"`
	Signals     []Signal       `json:"signals"`
	Traces      []TraceSummary `json:"traces"`      // the most recent
	ErrorTraces []TraceSummary `json:"errorTraces"` // the most recent with an error span
	ErrorLogs   []LogEntry     `json:"errorLogs"`   // the most recent error-level lines
	Metrics     []string       `json:"metrics"`     // metric names it reports
	// MetricsTruncated says it reports more than are listed.
	MetricsTruncated bool              `json:"metricsTruncated,omitempty"`
	Sources          map[string]string `json:"sources"`
	Warnings         []string          `json:"warnings,omitempty"`
}

// ServiceOverview reads the overview of one service.
func (c *Client) ServiceOverview(ctx context.Context, s Scope, name string, tr TimeRange) (*Overview, error) {
	if err := checkValue("service", name); err != nil {
		return nil, err
	}
	return c.overview(ctx, s, name, name, tr)
}

// ApplicationOverview reads the overview of one ikhnos application: the same things as for a service, over all of its
// services (the focus the Scope gets from the application).
func (c *Client) ApplicationOverview(ctx context.Context, s Scope, g *AppGroup, tr TimeRange) (*Overview, error) {
	fs, err := s.FocusOn(g)
	if err != nil {
		return nil, err
	}
	o, err := c.overview(ctx, fs, "", g.Name, tr)
	if err != nil {
		return nil, err
	}
	o.ID, o.Services = g.ID, fs.FocusServices
	return o, nil
}

// overview reads what a service (or, with no service named, whatever the Scope's focus selects) has. label is what the
// answer is called.
func (c *Client) overview(ctx context.Context, s Scope, name, label string, tr TimeRange) (*Overview, error) {
	o := &Overview{Name: label, Sources: map[string]string{SignalMetrics: SourceNotAllowed, SignalLogs: SourceNotAllowed, SignalTraces: SourceNotAllowed},
		Traces: []TraceSummary{}, ErrorTraces: []TraceSummary{}, ErrorLogs: []LogEntry{}, Metrics: []string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	has := map[string]bool{}
	run := func(signal string, do func() (bool, error)) {
		if !s.Allows(signal) {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			present, err := do()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				o.Sources[signal] = sourceState(err)
				o.Warnings = append(o.Warnings, fmt.Sprintf("%s: %v", signal, err))
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			o.Sources[signal] = SourceOK
			has[signal] = present
		}()
	}
	run(SignalTraces, func() (bool, error) {
		recent, _, err := c.SearchTraces(ctx, s, TraceFilter{Service: name}, tr, 10)
		if err != nil {
			return false, err
		}
		failed, _, err := c.SearchTraces(ctx, s, TraceFilter{Service: name, Status: "error"}, tr, 5)
		if err != nil {
			return false, err
		}
		mu.Lock()
		o.Traces, o.ErrorTraces = recent, failed
		mu.Unlock()
		return len(recent) > 0, nil
	})
	run(SignalLogs, func() (bool, error) {
		lines, _, err := c.Logs(ctx, s, LogFilter{Service: name, Severity: "error", Backward: true}, tr, 10)
		if err != nil {
			return false, err
		}
		one, _, err := c.Logs(ctx, s, LogFilter{Service: name, Backward: true}, tr, 1)
		if err != nil {
			return false, err
		}
		mu.Lock()
		o.ErrorLogs = lines
		mu.Unlock()
		return len(one) > 0, nil
	})
	run(SignalMetrics, func() (bool, error) {
		names, truncated, err := c.MetricNames(ctx, s, MetricFilter{Service: name}, tr, 100)
		if err != nil {
			return false, err
		}
		mu.Lock()
		o.Metrics, o.MetricsTruncated = names, truncated
		mu.Unlock()
		return len(names) > 0, nil
	})
	wg.Wait()
	if err := callerGone(ctx); err != nil {
		return nil, err
	}
	allFailed := firstErr != nil
	for _, v := range o.Sources {
		if v == SourceOK {
			allFailed = false
		}
	}
	if allFailed {
		return nil, firstErr
	}
	for _, sig := range Signals {
		if has[sig] {
			o.Signals = append(o.Signals, Signal(sig))
		}
	}
	if len(o.Signals) == 0 && firstErr == nil {
		return nil, errf(http.StatusNotFound, "no telemetry for %q in this range", label)
	}
	return o, nil
}
