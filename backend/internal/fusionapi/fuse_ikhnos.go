package fusionapi

import (
	"context"
	"fmt"
	"sort"
)

// fuseIkhnos reads the topology and the changes a fused read asked for and puts them on the fused object. It is a no-op
// for a read that asked for neither.
func (c *Client) fuseIkhnos(ctx context.Context, s Scope, f *Fused, looked []*Resource, opts FuseOptions, warn func(string, error), setSource func(string, string), note func(string, ...any)) {
	var view *TopologyView
	if opts.Topology || opts.Changes {
		if opts.Extras == nil {
			for _, k := range []string{SourceTopology, SourceChanges} {
				if (k == SourceTopology && opts.Topology) || (k == SourceChanges && opts.Changes) {
					setSource(k, SourceUnavailable)
					note("%s: this server has no topology to read", k)
				}
			}
			return
		}
		var err error
		if view, err = opts.Extras.Topology(ctx); err != nil {
			if opts.Topology {
				warn(SourceTopology, err)
			}
			if opts.Changes {
				warn(SourceChanges, err)
			}
			return
		}
	}
	ix := newTopoIndex(view) // (once for every resource, and for the changes)
	if opts.Topology {
		setSource(SourceTopology, SourceOK)
		n, amb := attachTopology(ix, s, looked, opts.Applications)
		if amb > 0 {
			note("topology: %d resource(s) were left unmatched because several services of ikhnos have their name and they carry no namespace or cluster to tell them apart", amb)
		}
		if n == 0 && amb == 0 && len(looked) > 0 {
			note("topology: none of the trace's services is known to ikhnos by that name, namespace and cluster")
		}
	}
	if !opts.Changes {
		return
	}
	setSource(SourceChanges, SourceOK)
	since, until := f.Start.Add(-opts.ChangesBefore), f.End.Add(opts.Pad)
	// The events of what the read joins: the trace's resources, or, narrowed to an application, the application's alone (what
	// the warnings say is not read is not read here either).
	about, only := f.Resources, ""
	if opts.Application != nil || opts.Spans.active() {
		about = looked
	}
	if opts.Application != nil {
		only = opts.Application.ID
	}
	var clusters []string
	for _, r := range about {
		if r.Cluster != "" {
			clusters = append(clusters, r.Cluster)
		}
	}
	clusters = uniqueInOrder(clusters)
	touched := touchedBy(ix, about, only)
	ids := touched.ids()
	if len(ids) == 0 {
		return // nothing of the trace is known to have events
	}
	// Only the events about the trace's own targets are read; what is left to drop afterwards is what the Scope may not see.
	// One more than is kept, to tell a window that held exactly the limit from one that held more.
	evs, err := opts.Extras.Changes(ctx, since, until, clusters, ids, changesRead+1)
	if err != nil {
		warn(SourceChanges, err)
		return
	}
	evs, cut := cutAt(evs, changesRead) // newest first
	if cut {
		note("changes: the window holds more than %d events; only the newest %d were read", changesRead, changesRead)
	}
	kept := relevantChanges(evs, ix, s, touched)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Time.Before(kept[j].Time) })
	if len(kept) > opts.MaxChanges {
		// keep the newest: what happened last is closest to the trace
		kept = kept[len(kept)-opts.MaxChanges:]
		note("changes: more than %d events; the newest are kept", opts.MaxChanges)
	}
	f.Changes = kept
}

// changesRead is how many events one fused read takes from the record before it scopes and trims them.
const changesRead = 2000

func (o *FuseOptions) ikhnosJoins(f *Fused) {
	if o.Topology {
		f.Sources[SourceTopology] = SourceNotRequested
		f.join(SourceTopology, JoinTopology, "associated, not proven: the service ikhnos knows by the same name, namespace and cluster, and the traffic it has seen to and from it as of now (the topology is not kept per trace). A resource that carries no namespace or cluster matches only when exactly one service has its name; when several do it has no match, and a warning says so")
	}
	if o.Changes {
		f.Sources[SourceChanges] = SourceNotRequested
		f.join(SourceChanges, JoinChangesWindow, fmt.Sprintf("associated, not proven: events ikhnos recorded about the trace's services, the traffic seen to and from them, the applications they are in, and its nodes and clusters, from %s before the trace to %s after it", o.ChangesBefore, o.Pad))
	}
}
