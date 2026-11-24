package fusionapi

import "slices"

// The joins of a fused read, one method of fuseRun each: what it reads, and where in the answer it puts it (see FuseTrace).

// promQLJoin is how the caller's queries are tied to the trace: those that name a resource variable for each resource, the
// others once for the whole trace (see FuseOptions.PromQL).
func promQLJoin(qs []PromQuery) string {
	per, whole := false, false
	for i := range qs {
		if qs[i].perResource {
			per = true
		} else {
			whole = true
		}
	}
	switch {
	case per && whole:
		return JoinResourceOrWindow
	case per:
		return JoinResourceWindow
	}
	return JoinWindow
}

// traceLogs reads the lines that carry the trace's id and puts each on its span (include=logs).
func (r *fuseRun) traceLogs() {
	if !r.allowed(r.opts.Logs, SignalLogs, SignalLogs) {
		return
	}
	narrowed := r.opts.Application != nil
	if len(r.logServices) == 0 && narrowed { // nothing to read lines of (an empty list would mean every service)
		r.setSource(SignalLogs, SourceOK)
		return
	}
	r.start(func() {
		release, err := acquire(r.ctx, r.sem) // (one read, but one of the read's all the same: see fuseConcurrency)
		if err != nil {
			r.warn(SignalLogs, err)
			return
		}
		lines, truncated, err := r.c.Logs(r.ctx, r.s, LogFilter{TraceID: r.f.TraceID, Services: r.logServices, Severity: r.opts.LogSeverity, Contains: r.opts.LogContains}, r.window, r.opts.MaxLogs)
		release()
		if err != nil {
			r.warn(SignalLogs, err)
			return
		}
		if narrowed {
			// The read named the services; a line is kept only if it is from one of the joined resources' own places (a service of
			// the same name in another namespace or cluster, on the same trace, is not part of what was asked for).
			lines = slices.DeleteFunc(lines, func(l LogEntry) bool { return !fromOneOf(l, r.looked) })
		}
		r.apply(func() {
			r.f.Sources[SignalLogs] = SourceOK
			attachLogs(r.f, lines, truncated)
		})
	})
}

// fromOneOf says whether a line is from one of the resources: the same service, in the same namespace and cluster where both say one.
func fromOneOf(l LogEntry, resources []*Resource) bool {
	for _, res := range resources {
		if l.Service != res.Service {
			continue
		}
		if (l.Namespace != "" && res.Namespace != "" && l.Namespace != res.Namespace) || (l.Cluster != "" && res.Cluster != "" && l.Cluster != res.Cluster) {
			continue
		}
		return true
	}
	return false
}

// contextLogs reads, for each looked-up resource, the lines it wrote in the trace's window without a trace id (include=context_logs).
// The resources are read together, in one query, and the answer split among them; a resource whose own lines that answer
// cannot tell exactly is then read on its own (see contextLogsAtOnce).
func (r *fuseRun) contextLogs() {
	if !r.allowed(r.opts.ContextLogs, SourceContextLogs, SignalLogs) {
		return
	}
	r.setSource(SourceContextLogs, SourceOK)
	if len(r.looked) < 2 {
		r.forEach(r.looked, SourceContextLogs, r.contextLogsOf)
		return
	}
	r.start(func() { r.forEach(r.contextLogsAtOnce(), SourceContextLogs, r.contextLogsOf) })
}

// contextFilter is what a resource's own read of its context lines asks for.
func (r *fuseRun) contextFilter(res *Resource) LogFilter {
	return LogFilter{Service: res.Service, Namespace: res.Namespace, Pod: res.Pod, Cluster: res.Cluster, NoTrace: true,
		Severity: r.opts.LogSeverity, Contains: r.opts.LogContains, Backward: true}
}

// contextLogsOf reads one resource's context lines on their own.
func (r *fuseRun) contextLogsOf(res *Resource) error {
	lines, truncated, err := r.c.Logs(r.ctx, r.s, r.contextFilter(res), r.window, r.opts.MaxContextLogs)
	if err != nil {
		return err
	}
	reverseLogs(lines) // newest first out of the store; a resource's lines read oldest first
	r.apply(func() { res.Logs, res.LogsTruncated = lines, truncated })
	return nil
}

// systemLogs reads the system namespaces' lines on the nodes the trace's pods (or, narrowed, the application's) ran on
// (include=system_logs).
func (r *fuseRun) systemLogs() {
	if !r.allowed(r.opts.SystemLogs, SourceSystemLogs, SignalLogs) {
		return
	}
	if r.s.AppLimited {
		// The lines of the system namespaces are about everything on a node, and carry no service of an application.
		r.setSource(SourceSystemLogs, SourceNotAllowed)
		r.note("system_logs: a token limited to ikhnos applications sees only its applications' own telemetry, and the system " +
			"namespaces' lines on a node belong to no application, so they are not read")
		return
	}
	nodesOf := r.f.Resources
	if r.opts.Application != nil {
		nodesOf = r.looked
	}
	r.start(func() {
		sl, warns, err := r.c.systemLogs(r.ctx, r.s, nodesOf, r.opts, r.window, r.sem)
		if err != nil {
			r.warn(SourceSystemLogs, err) // (the read of all the nodes failed, or, read one by one, every one of them did)
		}
		r.apply(func() {
			if err == nil {
				r.f.Sources[SourceSystemLogs] = SourceOK
				r.f.SystemLogs = sl
			}
			r.f.Warnings = append(r.f.Warnings, warns...)
		})
	})
}

// metrics reads each looked-up resource's series over the window (include=metrics); attachToSpans later cuts them to the spans.
func (r *fuseRun) metrics() {
	if !r.allowed(r.opts.Metrics, SignalMetrics, SignalMetrics) {
		return
	}
	r.setSource(SignalMetrics, SourceOK)
	if r.s.AppLimited && r.opts.MetricViews.Node {
		r.note("metrics: a node's own series name no service, so a token limited to ikhnos applications sees none of them; " +
			"the node view (metric_scope=node) is empty")
	}
	reads := newMetricReads(r.c, r.s, r.window, r.step, r.opts.MaxSeries)
	r.start(func() {
		// The views of all the resources are read at once first; what that read cannot answer exactly, each resource's own
		// reads then ask (see metricReads.prefetch).
		var filters []MetricFilter
		for _, res := range r.looked {
			filters = append(filters, metricFilters(res, r.opts)...)
		}
		if release, err := acquire(r.ctx, r.sem); err == nil {
			reads.prefetch(r.ctx, filters)
			release()
		}
		r.forEachLooked(SignalMetrics, func(res *Resource) error {
			series, truncated, err := resourceMetrics(r.ctx, res, r.opts, reads)
			if err != nil {
				return err
			}
			r.apply(func() { res.Metrics, res.MetricsTruncated = series, truncated })
			return nil
		})
	})
}

// promQL evaluates the caller's own queries (promql), for the looked-up resources and for the trace as a whole.
func (r *fuseRun) promQL() {
	if !r.allowed(len(r.opts.PromQL) > 0, SourcePromQL, SignalMetrics) {
		return
	}
	r.setSource(SourcePromQL, SourceOK)
	r.start(func() {
		global, warns, err := r.c.fusePromQL(r.ctx, r.s, r.f.Trace, r.looked, r.opts, r.qwin, r.qstep, r.sem, r.apply)
		r.apply(func() {
			r.f.Queries = global
			r.f.Warnings = append(r.f.Warnings, warns...)
		})
		if err != nil {
			r.warn(SourcePromQL, err)
		}
	})
}

// ikhnos adds what ikhnos knows about the trace's services: their topology and the changes recorded around the trace.
func (r *fuseRun) ikhnos() {
	if !r.opts.Topology && !r.opts.Changes {
		return
	}
	r.start(func() { r.c.fuseIkhnos(r.ctx, r.s, r.f, r.looked, r.opts, r.warn, r.setSource, r.note) })
}
