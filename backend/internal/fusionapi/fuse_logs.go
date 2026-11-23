package fusionapi

// The log lines of a fused read: those of the trace placed on their spans, and the system namespaces' lines of its nodes.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

func reverseLogs(l []LogEntry) {
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
}

// systemLogs reads the system namespaces' lines on the nodes the trace ran on (one read per cluster and node, at most
// maxSystemNodes), newest MaxSystemLogs of them in all, oldest first.
func (c *Client) systemLogs(ctx context.Context, s Scope, resources []*Resource, opts FuseOptions, window TimeRange) (*SystemLogs, []string, error) {
	type loc struct{ cluster, node string }
	var locs []loc
	seen := map[loc]bool{}
	for _, r := range resources {
		l := loc{r.Cluster, r.Node}
		if r.Node != "" && !seen[l] {
			seen[l] = true
			locs = append(locs, l)
		}
	}
	sl := &SystemLogs{Namespaces: opts.SystemNamespaces, Nodes: []string{}, Entries: []LogEntry{}}
	var warns []string
	visible := false
	for _, ns := range opts.SystemNamespaces {
		visible = visible || s.NamespaceVisible(ns)
	}
	if !visible {
		return sl, []string{"system_logs: this token cannot read the system namespaces (" + strings.Join(opts.SystemNamespaces, ", ") + ")"}, nil
	}
	if len(locs) == 0 {
		return sl, []string{"system_logs: the trace does not say which node its pods ran on"}, nil
	}
	if len(locs) > maxSystemNodes {
		warns = append(warns, fmt.Sprintf("system_logs: the trace ran on %d nodes; the first %d are read", len(locs), maxSystemNodes))
		locs = locs[:maxSystemNodes]
	}
	// One read per node; a node whose read fails is named in a warning and costs only its own lines. err is set only when
	// every node failed (sl is then empty, and still returned).
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failed []string
	var firstErr error
	for _, l := range locs {
		l := l
		wg.Add(1)
		go func() {
			defer wg.Done()
			lines, trunc, err := c.Logs(ctx, s, LogFilter{Namespaces: opts.SystemNamespaces, Node: l.node, Cluster: l.cluster,
				Severity: opts.LogSeverity, Contains: opts.LogContains, Backward: true}, window, opts.MaxSystemLogs)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, l.node)
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			sl.Nodes = append(sl.Nodes, l.node)
			sl.Entries = append(sl.Entries, lines...)
			sl.Truncated = sl.Truncated || trunc
		}()
	}
	wg.Wait()
	if len(failed) == len(locs) {
		return sl, warns, firstErr
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		warns = append(warns, fmt.Sprintf("system_logs: the lines of %d node(s) could not be read (%s): %v", len(failed), strings.Join(failed, ", "), firstErr))
	}
	sort.Strings(sl.Nodes)
	sort.SliceStable(sl.Entries, func(i, j int) bool { return sl.Entries[i].Time.After(sl.Entries[j].Time) })
	if len(sl.Entries) > opts.MaxSystemLogs {
		sl.Entries, sl.Truncated = sl.Entries[:opts.MaxSystemLogs], true
	}
	reverseLogs(sl.Entries)
	return sl, warns, nil
}

// attachLogs puts each line on the span it names and the rest in f.Logs.Unmatched.
func attachLogs(f *Fused, lines []LogEntry, truncated bool) {
	byID := make(map[string]*Span, len(f.Spans))
	for _, sp := range f.Spans {
		byID[sp.SpanID] = sp
	}
	f.Logs.Total, f.Logs.Truncated = len(lines), truncated
	for _, l := range lines {
		if sp := byID[normID(l.SpanID, 16)]; sp != nil && l.SpanID != "" {
			sp.Logs = append(sp.Logs, l)
			f.Logs.Matched++
			continue
		}
		f.Logs.Unmatched = append(f.Logs.Unmatched, l)
	}
}
