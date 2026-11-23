package fusionapi

// What is done to a fused read once everything is read: each resource's series cut to each span's own time, from one point
// budget, and the options that only narrow the answer applied.

import "time"

// windowAround is the trace's time widened by pad either side: at least a second long, and at most max (its newest part, with
// cut set).
func windowAround(tr *Trace, pad, max time.Duration) (w TimeRange, cut bool) {
	w = TimeRange{From: tr.Start.Add(-pad), To: tr.End.Add(pad)}
	if !w.From.Before(w.To) {
		w.To = w.From.Add(time.Second)
	}
	if w.To.Sub(w.From) > max {
		return w.newest(max), true
	}
	return w, false
}

// shape applies the options that only narrow the answer: the span filter and the attributes and events left out.
func shape(f *Fused, opts FuseOptions) {
	f.Warnings = uniqueInOrder(f.Warnings) // a store that is down is found down once for each of the trace's resources
	if opts.Spans.active() {
		kept := f.Spans[:0:0]
		for _, sp := range f.Spans {
			if opts.Spans.keeps(sp) {
				kept = append(kept, sp)
				continue
			}
			f.SpansOmitted++
			f.Logs.OnFilteredSpans += len(sp.Logs)
		}
		f.Spans = kept
	}
	for _, sp := range f.Spans {
		if opts.OmitAttributes {
			sp.Attributes = nil
		}
		if opts.OmitEvents {
			sp.Events = nil
		}
	}
	if opts.OmitAttributes {
		for _, r := range f.Resources {
			r.Attributes = nil
		}
	}
}

// spanBudget is what is left of the points all the spans of one fused read may carry between them (maxSpanPoints), metrics
// and the caller's queries alike.
type spanBudget struct{ left int }

// cut is the part of a series inside [from, to] (unix seconds), summarised; ok is false when no point falls inside. Its
// points come out of the budget; when there are not enough left it keeps the summary, loses the points and sets hit.
func (b *spanBudget) cut(m MetricSeries, from, to float64, hit *bool) (out MetricSeries, ok bool) {
	var in []Point
	for _, p := range m.Points {
		if p[0] >= from && p[0] <= to {
			in = append(in, p)
		}
	}
	if len(in) == 0 {
		return MetricSeries{}, false
	}
	out = MetricSeries{Name: m.Name, Category: m.Category, Labels: m.Labels, Points: in}
	out.summarise()
	if len(in) > b.left {
		out.Points, *hit = nil, true
	} else {
		b.left -= len(in)
	}
	return out, true
}

// attachToSpans gives each span the part of its resource's series that falls inside the span's own time widened by pad either
// side: the metrics (metrics) and the caller's per-resource queries (queries). It reads nothing more from the stores - the
// resource's series, already read over the whole trace, are cut - and only the spans the span filter keeps are given any, so
// the budget is not spent on spans the answer then leaves out. A resource whose read failed has nothing to cut; the others still
// are. It says, per kind, whether the budget ran out.
func attachToSpans(f *Fused, opts FuseOptions, metrics, queries bool) (metricsHit, queriesHit bool) {
	byKey := make(map[string]*Resource, len(f.Resources))
	for _, r := range f.Resources {
		byKey[r.Key] = r
	}
	b := &spanBudget{maxSpanPoints}
	for _, sp := range f.Spans {
		r := byKey[sp.Resource]
		if r == nil || (opts.Spans.active() && !opts.Spans.keeps(sp)) {
			continue
		}
		from, to := float64(sp.Start.Add(-opts.SpanPad).UnixNano())/1e9, float64(sp.End.Add(opts.SpanPad).UnixNano())/1e9
		if metrics {
			for _, m := range r.Metrics {
				if cut, ok := b.cut(m, from, to, &metricsHit); ok {
					sp.Metrics = append(sp.Metrics, cut)
				}
			}
		}
		if queries {
			for _, qr := range r.Queries {
				out := QueryResult{Name: qr.Name, Error: qr.Error, Series: []MetricSeries{}}
				for _, m := range qr.Series {
					if cut, ok := b.cut(m, from, to, &queriesHit); ok {
						out.Series = append(out.Series, cut)
					}
				}
				if len(out.Series) > 0 || out.Error != "" {
					sp.Queries = append(sp.Queries, out)
				}
			}
		}
	}
	return metricsHit, queriesHit
}
