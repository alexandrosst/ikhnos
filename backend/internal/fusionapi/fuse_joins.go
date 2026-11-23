package fusionapi

// The joins of a fused read, one method of fuseRun each: what it reads, and where in the answer it puts it (see FuseTrace).

// traceLogs reads the lines that carry the trace's id and puts each on its span (include=logs).
func (r *fuseRun) traceLogs() {
	if !r.allowed(r.opts.Logs, SignalLogs, SignalLogs) {
		return
	}
	if len(r.logServices) == 0 && r.opts.Application != nil { // nothing of the application to read lines of (an empty list would mean every service)
		r.setSource(SignalLogs, SourceOK)
		return
	}
	r.start(func() {
		lines, truncated, err := r.c.Logs(r.ctx, r.s, LogFilter{TraceID: r.f.TraceID, Services: r.logServices, Severity: r.opts.LogSeverity, Contains: r.opts.LogContains}, r.window, r.opts.MaxLogs)
		if err != nil {
			r.warn(SignalLogs, err)
			return
		}
		r.apply(func() {
			r.f.Sources[SignalLogs] = SourceOK
			attachLogs(r.f, lines, truncated)
		})
	})
}

// contextLogs reads, for each looked-up resource, the lines it wrote in the trace's window without a trace id (include=context_logs).
func (r *fuseRun) contextLogs() {
	if !r.allowed(r.opts.ContextLogs, SourceContextLogs, SignalLogs) {
		return
	}
	r.setSource(SourceContextLogs, SourceOK)
	r.forEachLooked(SourceContextLogs, func(res *Resource) error {
		lines, truncated, err := r.c.Logs(r.ctx, r.s, LogFilter{Service: res.Service, Namespace: res.Namespace, Pod: res.Pod, Cluster: res.Cluster, NoTrace: true,
			Severity: r.opts.LogSeverity, Contains: r.opts.LogContains, Backward: true}, r.window, r.opts.MaxContextLogs)
		if err != nil {
			return err
		}
		reverseLogs(lines) // newest first out of the store; a resource's lines read oldest first
		r.apply(func() { res.Logs, res.LogsTruncated = lines, truncated })
		return nil
	})
}

// systemLogs reads the system namespaces' lines on the nodes the trace's pods (or, narrowed, the application's) ran on
// (include=system_logs).
func (r *fuseRun) systemLogs() {
	if !r.allowed(r.opts.SystemLogs, SourceSystemLogs, SignalLogs) {
		return
	}
	nodesOf := r.f.Resources
	if r.opts.Application != nil {
		nodesOf = r.looked
	}
	r.start(func() {
		sl, warns, err := r.c.systemLogs(r.ctx, r.s, nodesOf, r.opts, r.window)
		if err != nil {
			r.warn(SourceSystemLogs, err) // (every node failed; one that did not would keep its lines)
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
	reads := newMetricReads(r.c, r.s, r.window, r.step, r.opts.MaxSeries)
	r.forEachLooked(SignalMetrics, func(res *Resource) error {
		series, truncated, err := resourceMetrics(r.ctx, res, r.opts, reads)
		if err != nil {
			return err
		}
		r.apply(func() { res.Metrics, res.MetricsTruncated = series, truncated })
		return nil
	})
}

// promQL evaluates the caller's own queries (promql), for the looked-up resources and for the trace as a whole.
func (r *fuseRun) promQL() {
	if !r.allowed(len(r.opts.PromQL) > 0, SourcePromQL, SignalMetrics) {
		return
	}
	if !r.s.Unrestricted() {
		r.setSource(SourcePromQL, SourceNotAllowed)
		r.note("promql: your own queries can only be run by a caller whose access is not limited to certain namespaces or clusters")
		return
	}
	r.setSource(SourcePromQL, SourceOK)
	r.start(func() {
		global, warns, err := r.c.fusePromQL(r.ctx, r.s, r.f.Trace, r.looked, r.opts, r.qwin, r.qstep, r.apply)
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
