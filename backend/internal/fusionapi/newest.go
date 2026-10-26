package fusionapi

import (
	"context"
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

// NewestTrace is the start time of the newest trace the traces store holds inside the window, ok false when it holds none.
func (c *Client) NewestTrace(ctx context.Context, s Scope, window time.Duration) (t time.Time, ok bool, err error) {
	if err := s.needUnrestricted("the newest stored trace"); err != nil {
		return time.Time{}, false, err
	}
	now := c.now()
	traces, _, err := c.SearchTraces(ctx, s, TraceFilter{}, TimeRange{From: now.Add(-window), To: now}, 1)
	if err != nil || len(traces) == 0 || traces[0].Start.IsZero() {
		return time.Time{}, false, err
	}
	return traces[0].Start, true, nil
}
