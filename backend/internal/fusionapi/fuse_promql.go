package fusionapi

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// errNotEvaluated is the result of an expansion past maxPromRuns: not the store's failure, the read's own limit.
var errNotEvaluated = fmt.Errorf("not evaluated: this read evaluates at most %d distinct queries", maxPromRuns)

type promRun struct {
	series    []MetricSeries
	truncated bool
	err       error
}

// fusePromQL evaluates the caller's queries for a trace. Queries about a resource are put on it (looked is the resources
// worth a lookup); the others are returned. The warnings say what was skipped; firstErr is the first store failure.
// sem bounds the reads in flight (shared with the read's other joins); locked runs a write to the resources under the read's lock.
func (c *Client) fusePromQL(ctx context.Context, s Scope, tr *Trace, looked []*Resource, opts FuseOptions, w TimeRange, step time.Duration, sem chan struct{}, locked func(func())) (global []QueryResult, warns []string, firstErr error) {
	rangeVar := strconv.Itoa(int(math.Ceil(w.To.Sub(w.From).Seconds()))) + "s"
	common := map[string]string{"trace_id": tr.TraceID, "range": rangeVar}
	if g := opts.Application; g != nil {
		common["application"], common["application_id"] = g.Name, g.ID
	}

	type slot struct {
		q    *PromQuery
		r    *Resource // nil: not about a resource
		expr string
		app  bool // narrowed to the application (narrowToApplication)
	}
	var slots []slot
	runs := map[string]*promRun{}
	var order []string
	skipped := map[string][]string{} // query name -> why, for the warning
	over := map[string]bool{}        // the queries with an expansion past maxPromRuns

	add := func(q *PromQuery, r *Resource, vars map[string]string) {
		expr, skip := q.expand(vars)
		if skip != "" {
			label := "the trace"
			if r != nil {
				label = r.Key
			}
			skipped[q.Name] = append(skipped[q.Name], label+": "+skip)
			return
		}
		app := false
		if wide := r == nil || (q.uses("namespace", "cluster") && !q.uses("service", "pod", "workload")); wide && !opts.PromQLAsWritten && (opts.Application != nil || len(opts.PromQLServices) > 0) {
			// A query that is not about one service or pod (it names none of them, or only the namespace or cluster a resource is
			// in) would read every service there: narrowed, it reads what the read is about - the application's own, and only the
			// services of promql_services when they are named.
			narrowed, err := narrowQuery(expr, opts)
			if err != nil {
				if _, ok := runs[expr]; !ok {
					runs[expr] = &promRun{err: err}
				}
				slots = append(slots, slot{q, r, expr, false})
				return
			}
			expr, app = narrowed, opts.Application != nil
		}
		if _, ok := runs[expr]; !ok {
			if len(runs) >= maxPromRuns {
				over[q.Name] = true
				runs[expr] = &promRun{err: errNotEvaluated} // answered without asking the store, so the caller sees which went unasked
			} else {
				runs[expr] = &promRun{}
				order = append(order, expr)
			}
		}
		slots = append(slots, slot{q, r, expr, app})
	}
	services := map[string]bool{}
	for _, n := range opts.PromQLServices {
		services[n] = true
	}
	// The queries about the whole trace first: each is one expansion, and one per-resource query of a trace with many pods could
	// otherwise take every slot before them.
	for i := range opts.PromQL {
		if q := &opts.PromQL[i]; !q.perResource {
			add(q, nil, common)
		}
	}
	for i := range opts.PromQL {
		q := &opts.PromQL[i]
		if !q.perResource {
			continue
		}
		for _, r := range looked {
			if len(services) > 0 && !services[r.Service] {
				continue
			}
			vars := resourceVars(r)
			for k, v := range common {
				vars[k] = v
			}
			add(q, r, vars)
		}
	}
	if len(over) > 0 {
		names := make([]string, 0, len(over))
		for n := range over {
			names = append(names, n)
		}
		sort.Strings(names)
		warns = append(warns, fmt.Sprintf("promql: this read evaluates at most %d distinct queries; some expansions of %s were not evaluated (their results say so)", maxPromRuns, strings.Join(names, ", ")))
	}
	names := make([]string, 0, len(skipped))
	for n := range skipped {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		list := firstOf(skipped[n], 3, func(rest int) string { return fmt.Sprintf("and %d more", rest) })
		warns = append(warns, fmt.Sprintf("promql %s: not evaluated for %s", n, strings.Join(list, "; ")))
	}

	var wg sync.WaitGroup
	for _, expr := range order {
		run := runs[expr]
		expr := expr
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquire(ctx, sem)
			if err != nil {
				run.err = err
				return
			}
			defer release()
			run.series, run.truncated, run.err = c.promRange(ctx, s, expr, w, step, opts.MaxSeries)
		}()
	}
	wg.Wait()

	// A query narrowed to the application matches it by service_name alone (see narrowToApplication). A resource that is
	// part of the application by its workload's name may have its series under another service_name, or none, so a query
	// that came back with no series at all may have missed it: the warning says so for such a query only, and names them.
	missed := map[string][]string{} // query name -> resource keys
	for _, sl := range slots {
		if run := runs[sl.expr]; !sl.app || run.err != nil || len(run.series) > 0 {
			continue
		}
		of := tr.Resources // the query for the trace as a whole is about every resource of the application in it
		if sl.r != nil {
			of = []*Resource{sl.r}
		}
		for _, res := range of {
			if opts.Application.byWorkloadOnly(res) {
				missed[sl.q.Name] = append(missed[sl.q.Name], res.Key)
			}
		}
	}
	for _, n := range slices.Sorted(maps.Keys(missed)) {
		keys := uniqueInOrder(missed[n])
		list := firstOf(keys, 3, func(rest int) string { return fmt.Sprintf("and %d more", rest) })
		warns = append(warns, fmt.Sprintf("promql %s: no series. Narrowed to application %s, the query matches it "+
			"by service_name only, and %d of its resources here are part of it by their workload's name, not their "+
			"service.name (%s): their series are read only where they carry one of the application's names as "+
			"service_name. A per-resource query that selects on ${workload}, or promql_as_written=true, reads them",
			n, opts.Application.Name, len(keys), strings.Join(list, ", ")))
	}

	for _, sl := range slots {
		run := runs[sl.expr]
		res := QueryResult{Name: sl.q.Name, Query: sl.expr, Series: run.series, Truncated: run.truncated}
		if run.err != nil {
			res.Error = run.err.Error()
			if firstErr == nil && run.err != errNotEvaluated {
				firstErr = run.err
			}
		}
		if res.Series == nil {
			res.Series = []MetricSeries{}
		}
		if sl.r == nil {
			global = append(global, res)
		} else {
			locked(func() { sl.r.Queries = append(sl.r.Queries, res) }) // (the other joins write the same resources)
		}
	}
	return global, warns, firstErr
}
