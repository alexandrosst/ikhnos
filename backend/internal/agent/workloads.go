package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

// What the release runs, as the controller counts it. The controller that keeps this agent installed writes, under status.workloads of the
// IkhnosAgent, how many pods of each workload of the release (the agent, the collectors, Kepler, DCGM, the node probe) are ready. The agent
// reads it, the way it reads status.relay (it may get that resource only when the relay is on), and puts it in its diagnostics, so that the
// server can say that Kepler runs on two of the three nodes it is asked to. It is a reading, never an action: the agent changes nothing.

// WorkloadStore is a RelayStore that can also say what the release runs.
type WorkloadStore interface {
	Workloads(ctx context.Context) ([]relay.Workload, error)
}

// WorkloadsHeldBackAfter is how long a workload is seen with fewer pods ready than it asks for before the agent says it is held back. A pod
// that has just been asked for takes a while to start, and is not a problem until it has had that while.
const WorkloadsHeldBackAfter = 3 * time.Minute

// maxWorkloadsReported bounds what the agent reports: the chart has a handful, so more is a malformed status.
const maxWorkloadsReported = 16

func (s *resourceRelay) Workloads(ctx context.Context) ([]relay.Workload, error) {
	raw, err := s.kube.CoreV1().RESTClient().Get().AbsPath(s.path).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the status of the IkhnosAgent %s: %w", s.name, err)
	}
	var got struct {
		Status struct {
			Workloads []relay.Workload `json:"workloads"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("the status of the IkhnosAgent %s cannot be read: %w", s.name, err)
	}
	return got.Status.Workloads, nil
}

// ensure the resource store can say it.
var _ WorkloadStore = (*resourceRelay)(nil)

// workloadState is the last reading and, per workload, since when it has had fewer pods ready than it asks for.
type workloadState struct {
	mu       sync.Mutex
	list     []relay.Workload
	notReady map[string]time.Time
	failures int
}

// noteWorkloads takes a reading made at now. A workload that is complete is forgotten, so that its next shortfall starts a new count.
func (r *runner) noteWorkloads(ws []relay.Workload, now time.Time) {
	st := &r.wl
	st.mu.Lock()
	defer st.mu.Unlock()
	st.failures = 0
	if len(ws) > maxWorkloadsReported {
		ws = ws[:maxWorkloadsReported]
	}
	st.list = append([]relay.Workload(nil), ws...)
	next := map[string]time.Time{}
	for _, w := range ws {
		if w.Ready >= w.Wanted {
			continue
		}
		if since, ok := st.notReady[w.Name]; ok {
			next[w.Name] = since
		} else {
			next[w.Name] = now
		}
	}
	st.notReady = next
}

// workloadsFailed is a reading that did not work. After a few in a row what was known is let go rather than stated for ever.
func (r *runner) workloadsFailed(err error) {
	st := &r.wl
	st.mu.Lock()
	defer st.mu.Unlock()
	st.failures++
	if st.failures == 1 {
		r.log.Warn("could not read what the controller says the release runs", "err", err)
	}
	if st.failures >= 3 {
		st.list, st.notReady = nil, nil
	}
}

// workloadReport is what the diagnostics say about what the release runs, in a stable order.
func (r *runner) workloadReport(now time.Time) []*ikhnosv1.WorkloadStatus {
	st := &r.wl
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.list) == 0 {
		return nil
	}
	out := make([]*ikhnosv1.WorkloadStatus, 0, len(st.list))
	for _, w := range st.list {
		ws := &ikhnosv1.WorkloadStatus{Name: clipMsg(w.Name, 80), Kind: clipMsg(w.Kind, 20), Component: clipMsg(w.Component, 60), Wanted: uint32(max(w.Wanted, 0)), Ready: uint32(max(w.Ready, 0)), RollingOut: w.RollingOut}
		if since, ok := st.notReady[w.Name]; ok {
			ws.NotReadySeconds = uint32(max(now.Sub(since), 0) / time.Second)
		}
		out = append(out, ws)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// workloadsLoop reads what the release runs, for as long as the agent runs. It is started only where the relay is on, which is where the agent
// may read the resource.
func (r *runner) workloadsLoop(ctx context.Context, store WorkloadStore, every time.Duration) {
	read := func() {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ws, err := store.Workloads(c)
		if err != nil {
			r.workloadsFailed(err)
			return
		}
		r.noteWorkloads(ws, time.Now())
	}
	read()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			read()
		}
	}
}
