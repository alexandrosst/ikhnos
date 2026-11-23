package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// NewestWindow is how far back the stores are asked for their newest entry: a store that has held nothing in this long is
// reported as having nothing, which is what "no data lately" needs; the question is a single newest entry, so the window costs
// the stores nothing a short one would not.
const NewestWindow = 6 * time.Hour

// NewestLog is the time of the newest log line the logs store holds inside the window (backward, one line), ok false when it
// holds none. Like HeadMaxTime it says something about every sender, so only a Scope with no limit may ask.
func (c *Client) NewestLog(ctx context.Context, s Scope, window time.Duration) (t time.Time, ok bool, err error) {
	if err := s.needUnrestricted("the newest stored log line"); err != nil {
		return time.Time{}, false, err
	}
	now := c.now()
	entries, _, err := c.Logs(ctx, s, LogFilter{Backward: true}, TimeRange{From: now.Add(-window), To: now}, 1)
	if err != nil || len(entries) == 0 {
		return time.Time{}, false, err
	}
	return entries[0].Time, true, nil
}

// newestTraceSteps are the windows the newest trace is looked for in, shortest first. Tempo's search does not return the newest traces
// first: over a wide window it answers with the first ones it happens to find, in whichever block it reads first, so one trace asked for
// over six hours can be many minutes old while traces are arriving now. Asking the short windows first finds a recent trace if there is one.
var newestTraceSteps = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

// newestTraceSample is how many traces a window is asked for, of which the newest is taken: with only one, a window with many traces
// would still answer with an arbitrary one of them.
const newestTraceSample = 20

// NewestTrace is the start time of the newest trace the traces store holds inside the window, ok false when it holds none. It asks the
// short windows first (see newestTraceSteps), so the answer is as recent as the shortest window that holds any trace.
func (c *Client) NewestTrace(ctx context.Context, s Scope, window time.Duration) (t time.Time, ok bool, err error) {
	if err := s.needUnrestricted("the newest stored trace"); err != nil {
		return time.Time{}, false, err
	}
	now := c.now()
	steps := make([]time.Duration, 0, len(newestTraceSteps)+1)
	for _, w := range newestTraceSteps {
		if w < window {
			steps = append(steps, w)
		}
	}
	steps = append(steps, window)
	for _, w := range steps {
		traces, _, err := c.SearchTraces(ctx, s, TraceFilter{}, TimeRange{From: now.Add(-w), To: now}, newestTraceSample)
		if err != nil {
			return time.Time{}, false, err
		}
		for _, tr := range traces {
			if tr.Start.After(t) {
				t, ok = tr.Start, true
			}
		}
		if ok {
			return t, true, nil
		}
	}
	return time.Time{}, false, nil
}

// HeadMaxTime is the timestamp of the newest sample Prometheus holds, from its TSDB status (GET /api/v1/status/tsdb:
// headStats.maxTime, in milliseconds). ok is false when nothing has been stored yet (an empty head reports the minimum
// int64). It is the cheapest honest "has anything arrived": one number Prometheus already keeps, so it does not depend on which
// series a sender produces and costs the same however much is stored. Only a Scope with no namespace or cluster limit may ask,
// since it says something about every sender.
func (c *Client) HeadMaxTime(ctx context.Context, s Scope) (t time.Time, ok bool, err error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return time.Time{}, false, err
	}
	if err := s.needUnrestricted("the newest stored sample"); err != nil {
		return time.Time{}, false, err
	}
	data, err := c.prom(ctx, "/api/v1/status/tsdb", nil)
	if err != nil {
		return time.Time{}, false, err
	}
	var d struct {
		HeadStats struct {
			NumSeries int64 `json:"numSeries"`
			MaxTime   int64 `json:"maxTime"`
		} `json:"headStats"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return time.Time{}, false, errf(http.StatusBadGateway, "%s: unreadable TSDB status", storeProm)
	}
	// An empty head has minTime = MaxInt64 and maxTime = MinInt64; anything not after 2001 is not a sample time.
	if d.HeadStats.NumSeries <= 0 || d.HeadStats.MaxTime < 1e12 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(d.HeadStats.MaxTime).UTC(), true, nil
}
