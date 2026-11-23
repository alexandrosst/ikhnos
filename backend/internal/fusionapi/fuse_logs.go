package fusionapi

// The log lines of a fused read: those of the trace placed on their spans, and the system namespaces' lines of its nodes.

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
)

func reverseLogs(l []LogEntry) {
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
}

// sysLoc is one node a trace ran on, in its cluster ("" when the trace does not say which).
type sysLoc struct{ cluster, node string }

// systemLogs reads the system namespaces' lines on the nodes the trace ran on (at most maxSystemNodes of them), newest
// MaxSystemLogs of them in all, oldest first. Every read takes a slot of sem, the fused read's bound on its reads.
//
// The nodes are read together, in one query that keeps a line of any of them: the answer is the newest lines of them all,
// which is what one read per node, merged, keeps too. Where the two could differ the nodes are read one by one as they
// used to be (see systemLogsAtOnce).
func (c *Client) systemLogs(ctx context.Context, s Scope, resources []*Resource, opts FuseOptions, window TimeRange,
	sem chan struct{}) (*SystemLogs, []string, error) {
	var locs []sysLoc
	for _, r := range resources {
		if r.Node != "" {
			locs = append(locs, sysLoc{r.Cluster, r.Node})
		}
	}
	locs = uniqueInOrder(locs)
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
	base := LogFilter{Namespaces: opts.SystemNamespaces, Severity: opts.LogSeverity, Contains: opts.LogContains,
		Backward: true}
	if len(locs) > 1 {
		entries, truncated, exact, err := c.systemLogsAtOnce(ctx, s, base, locs, window, opts.MaxSystemLogs, sem)
		if err != nil && !batchRefused(err) {
			// The read of every node failed as each node's own read would have (a store that is down or too slow), so the
			// part fails as a whole, as it did when every node failed, without spending the time again on each.
			return sl, warns, err
		}
		// (Refused for what it is, the query of all the nodes is not; the nodes are read one by one below.)
		if exact {
			for _, l := range locs {
				sl.Nodes = append(sl.Nodes, l.node)
			}
			sort.Strings(sl.Nodes)
			reverseLogs(entries)
			if entries != nil {
				sl.Entries = entries
			}
			sl.Truncated = truncated
			return sl, warns, nil
		}
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
			var lines []LogEntry
			var trunc bool
			release, err := acquire(ctx, sem)
			if err == nil {
				f := base
				f.Node, f.Cluster = l.node, l.cluster
				lines, trunc, err = c.Logs(ctx, s, f, window, opts.MaxSystemLogs)
				release()
			}
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

// systemLogsAtOnce reads the lines of every node in one query (base's, plus a filter that keeps a line of any of the
// nodes), the newest limit of them in all, newest first. exact says the answer is the one a read per node, merged, gives;
// when it is not, the caller reads the nodes one by one instead. It is not when:
//
//   - one line can be of two of the nodes (a node named with no cluster beside the same name with one): a read per node
//     returns such a line twice, a read of all of them once;
//   - a node's name or cluster is not a value a query may carry: its own read fails alone, which one read cannot tell;
//   - Loki's answer was full and some of its lines are not the caller's to see: the reads per node would each have
//     filled their own limit, so they reach lines further back than the one read did.
//
// err is the read's own failure; the caller tells a refusal of the query (batchRefused) from a store that failed.
func (c *Client) systemLogsAtOnce(ctx context.Context, s Scope, base LogFilter, locs []sysLoc, window TimeRange, limit int,
	sem chan struct{}) (entries []LogEntry, truncated, exact bool, err error) {
	named := map[string]int{}
	unclustered := map[string]bool{}
	d := labelSyntax
	var alts [][]string
	for _, l := range locs {
		if checkValue("node", l.node) != nil || (l.cluster != "" && checkValue("cluster", l.cluster) != nil) {
			return nil, false, false, nil
		}
		named[l.node]++
		unclustered[l.node] = unclustered[l.node] || l.cluster == ""
		c := []string{d.eq(lblNode, l.node)}
		if l.cluster != "" {
			c = append(c, d.eq(d.cluster, l.cluster))
		}
		alts = append(alts, c)
	}
	for n, k := range named {
		if k > 1 && unclustered[n] {
			return nil, false, false, nil
		}
	}
	q, err := base.logQL(s)
	if err != nil {
		return nil, false, false, err // (a read per node would build the same query, and fail the same way)
	}
	q += " | " + d.either(alts) // (locs holds each node once, so no alternative is written twice)
	release, err := acquire(ctx, sem)
	if err != nil {
		return nil, false, false, err
	}
	lines, err := c.lokiRaw(ctx, s, q, true, window, limit+1)
	release()
	if err != nil {
		return nil, false, false, err
	}
	if len(lines) > limit && slices.ContainsFunc(lines, func(l lokiLine) bool { return !l.kept }) {
		slog.Debug("fused read: the system lines of all the nodes were a full answer holding lines the caller may not see; "+
			"each node is read again on its own", "nodes", len(locs))
		return nil, false, false, nil
	}
	entries, truncated = cutLines(lines, true, limit)
	return entries, truncated, true, nil
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

// lokiMaxEntries is the most lines one Loki read may ask for. Loki refuses a limit above its max_entries_limit_per_query,
// which the chart's Loki (internal/chart/ikhnos-fusion/templates/loki-config.yaml) leaves at Loki's default, 5000.
const lokiMaxEntries = 5000

// contextLogsAtOnce reads the context lines of the looked-up resources in one query and gives each resource the lines its
// own read would have returned. It returns the resources it could not answer so, which the caller reads one by one.
//
// Only a resource that names its service and namespace is read with the others, so that the query opens no more streams
// than their own reads would: the rest (a pod with no service, a service in no namespace) are read on their own.
//
// The query keeps a line of any of the resources, newest first, up to (max+1) lines per resource (at most lokiMaxEntries).
// A resource's own read would have kept its newest max+1 lines and cut them to max; the lines of a resource in this answer
// are its newest ones too, so where there are max+1 of them, or the answer was not full (so it holds every line of every
// resource), they are what its own read had, and the same cut is made of them. A resource with fewer in a full answer
// may have had more further back, and is read on its own. When Loki refuses the query for what it is (batchRefused),
// every resource is read on its own; when the read fails otherwise, each own read would have failed the same way, and the
// failure is reported for each resource, as those reads would have.
func (r *fuseRun) contextLogsAtOnce() (rest []*Resource) {
	most := r.opts.MaxContextLogs
	var batch []*Resource
	for _, res := range r.looked {
		if res.Service == "" || res.Namespace == "" {
			rest = append(rest, res)
			continue
		}
		if _, err := r.contextFilter(res).logQL(r.s); err != nil {
			rest = append(rest, res) // its own read fails as it always did, and alone
			continue
		}
		batch = append(batch, res)
	}
	if len(batch) < 2 {
		return r.looked
	}
	q, err := r.contextQuery(batch)
	if err != nil {
		return r.looked
	}
	limit := min(len(batch)*(most+1), lokiMaxEntries)
	release, err := acquire(r.ctx, r.sem)
	if err != nil {
		return r.looked // (each own read then waits for a slot in its turn, and fails as it would have)
	}
	lines, err := r.c.lokiRaw(r.ctx, r.s, q, true, r.window, limit)
	release()
	if err != nil && !batchRefused(err) {
		for range batch {
			r.warn(SourceContextLogs, err)
		}
		return rest
	}
	if err != nil || slices.ContainsFunc(lines, func(l lokiLine) bool { return !l.decoded }) {
		// (A line Loki sent that cannot be read cannot be told to a resource either, yet it took a place in a resource's limit.)
		return r.looked
	}
	full := len(lines) >= limit
	again := 0
	for _, res := range batch {
		var mine []lokiLine
		for _, l := range lines {
			if contextLineOf(l.e, res) {
				mine = append(mine, l)
			}
		}
		if full && len(mine) <= most {
			rest = append(rest, res)
			again++
			continue
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].e.Time.After(mine[j].e.Time) })
		if len(mine) > most+1 {
			mine = mine[:most+1] // what its own read, asking for most+1, would have been answered
		}
		entries, truncated := cutLines(mine, true, most)
		reverseLogs(entries) // newest first out of the store; a resource's lines read oldest first
		r.apply(func() { res.Logs, res.LogsTruncated = entries, truncated })
	}
	if again > 0 {
		slog.Debug("fused read: the context lines of all the resources were a full answer; some are read again on their own",
			"resources", len(batch), "again", again)
	}
	return rest
}

// contextQuery is one query for the context lines of all the resources, each of which names its service and namespace:
// the selector of their services, namespaces and (when every one names one) pods, so that Loki opens only those streams,
// the filters every one of their reads has (and the Scope's), and then a filter that keeps a line of any one of them by
// its service, namespace, pod and cluster, each where the resource names one.
func (r *fuseRun) contextQuery(resources []*Resource) (string, error) {
	f := LogFilter{NoTrace: true, Severity: r.opts.LogSeverity, Contains: r.opts.LogContains, Backward: true}
	var services, namespaces, pods []string
	allPods := true
	for _, res := range resources {
		allPods = allPods && res.Pod != ""
		services, namespaces, pods = append(services, res.Service), append(namespaces, res.Namespace), append(pods, res.Pod)
	}
	if services = uniqueInOrder(services); len(services) == 1 {
		f.Service = services[0]
	} else {
		f.Services = services
	}
	if namespaces = uniqueInOrder(namespaces); len(namespaces) == 1 {
		f.Namespace = namespaces[0]
	} else {
		f.Namespaces = namespaces
	}
	if pods = uniqueInOrder(pods); allPods && len(pods) == 1 {
		f.Pod = pods[0]
	} else if allPods {
		f.pods = pods
	}
	q, err := f.logQL(r.s)
	if err != nil {
		return "", err
	}
	var alts [][]string
	for _, res := range resources {
		var c []string
		for _, kv := range [][2]string{{lblService, res.Service}, {lblNamespace, res.Namespace}, {lblPod, res.Pod},
			{lblCluster, res.Cluster}} {
			if kv[1] != "" {
				c = append(c, labelSyntax.eq(kv[0], kv[1]))
			}
		}
		if len(c) == 0 {
			// (Not reached: every resource here names its service and namespace. An empty alternative would keep every line.)
			return "", badRequest("a resource with nothing to select it by cannot be read with others")
		}
		alts = append(alts, c)
	}
	// (Two resources that differ only in what a line cannot tell, such as their instance, are one alternative, which
	// either writes once.)
	return q + " | " + labelSyntax.either(alts), nil
}

// contextLineOf says whether a line is one a resource's own read of its context lines would have matched: the same
// service, namespace and pod (the stream's labels, which that read selects by) and cluster, each where the resource names
// one. The line's fields are read from the stream as that read's matchers see them (see lokiEntry).
func contextLineOf(e LogEntry, res *Resource) bool {
	return (res.Service == "" || e.Service == res.Service) && (res.Namespace == "" || e.Namespace == res.Namespace) &&
		(res.Pod == "" || e.Pod == res.Pod) && (res.Cluster == "" || e.Cluster == res.Cluster)
}
