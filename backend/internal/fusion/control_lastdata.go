package fusion

import (
	"context"
	"sync"
	"time"

	"ikhnos/internal/fusionapi"
)

// lastData is when each store last held something: Prometheus's newest sample (which is also the status's LastDataAt), Loki's newest
// line and Tempo's newest trace, each asked only while its store is up, the three together, at most once per fusionLastDataEvery. A
// failed ask, or a store that answers "nothing in the last hours", leaves what was known (data seen longer ago than the window no
// longer answers, but it was still the last data). Anything that goes wrong is simply "unknown": it is a nicety next to the state.
func (f *FusionControl) lastData(ctx context.Context, st FusionStatus) (*time.Time, *StoreTimes, *fusionapi.GatewayTraffic) {
	f.lastDataMu.Lock()
	defer f.lastDataMu.Unlock()
	if now := f.Clock(); f.lastDataTried.IsZero() || now.Sub(f.lastDataTried) >= fusionLastDataEvery {
		f.lastDataTried = now
		c := f.DataClient()
		scope := fusionapi.AllSignals()
		var wg sync.WaitGroup
		var mt, lt, tt time.Time
		var mok, lok, tok bool
		ask := func(up bool, run func(context.Context) (time.Time, bool, error), t *time.Time, ok *bool) {
			if !up {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				qctx, cancel := context.WithTimeout(ctx, fusionKubeTimeout)
				defer cancel()
				if v, found, err := run(qctx); err == nil && found {
					*t, *ok = v, true
				}
			}()
		}
		// Prometheus's own TSDB status, not a query over series: nothing is scraped here (everything is pushed over OTLP), so
		// `up` does not exist, and target_info - once the obvious choice - is written only for a resource that has a
		// service.name or service.instance.id, which the infrastructure metrics (hostmetrics, kubelet, cluster state) do not
		// have; a store full of data then read as "waiting for the first". See Client.HeadMaxTime.
		ask(st.ComponentUp(ComponentMetrics), func(x context.Context) (time.Time, bool, error) { return c.HeadMaxTime(x, scope) }, &mt, &mok)
		ask(st.ComponentUp(ComponentLogs), func(x context.Context) (time.Time, bool, error) {
			return c.NewestLog(x, scope, fusionapi.NewestWindow)
		}, &lt, &lok)
		ask(st.ComponentUp(ComponentTraces), func(x context.Context) (time.Time, bool, error) {
			return c.NewestTrace(x, scope, fusionapi.NewestWindow)
		}, &tt, &tok)
		if st.ComponentUp(ComponentMetrics) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				qctx, cancel := context.WithTimeout(ctx, fusionKubeTimeout)
				defer cancel()
				tr, err := c.GatewayTraffic(qctx, scope)
				if err != nil {
					tr = nil
				}
				f.traffic = tr // one goroutine writes it, and lastDataMu is held until Wait returns
			}()
		}
		wg.Wait()
		if mok {
			f.lastDataSeen = mt
		}
		if lok {
			f.lastLogSeen = lt
		}
		if tok {
			f.lastTraceSeen = tt
		}
	}
	var at *time.Time
	if !f.lastDataSeen.IsZero() {
		t := f.lastDataSeen
		at = &t
	}
	var per StoreTimes
	var have bool
	for _, x := range []struct {
		up   bool
		seen time.Time
		to   **time.Time
	}{
		{st.ComponentUp(ComponentMetrics), f.lastDataSeen, &per.Metrics},
		{st.ComponentUp(ComponentLogs), f.lastLogSeen, &per.Logs},
		{st.ComponentUp(ComponentTraces), f.lastTraceSeen, &per.Traces},
	} {
		if x.up && !x.seen.IsZero() {
			t := x.seen
			*x.to, have = &t, true
		}
	}
	if !st.ComponentUp(ComponentMetrics) {
		at = nil // as before: the metrics store's own time is told only while it is up
	}
	var traffic *fusionapi.GatewayTraffic
	if st.ComponentUp(ComponentMetrics) && f.traffic != nil {
		traffic = copyTraffic(f.traffic)
	}
	if !have {
		return at, nil, traffic
	}
	return at, &per, traffic
}

// rememberedLastData is what lastDataAt last found, without asking again.
func (f *FusionControl) rememberedLastData() *time.Time {
	f.lastDataMu.Lock()
	defer f.lastDataMu.Unlock()
	if f.lastDataSeen.IsZero() {
		return nil
	}
	t := f.lastDataSeen
	return &t
}
