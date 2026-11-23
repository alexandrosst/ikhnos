package fusionapi

import (
	"context"
	"slices"
	"sort"
)

// Names beside ids. The telemetry carries the ids of the organisation and of the clusters (ikhnos_org_id, ikhnos.cluster.id),
// which do not change when either is renamed, and so the answers are keyed by them; but people know a cluster by the name it
// was given in ikhnos, and a reader of an answer should not have to ask another route what cl-7f3a is. The server reads the
// names once per request (Namer) and the answers put them beside the ids: on each resource (Resource.ClusterName), in the
// context of a fused read (Fused.Context) and in a dictionary beside the log lines that name clusters.

// Names are what ikhnos calls the organisation and its clusters, as the server last read them. A nil *Names is a server
// that does not know them, and the answers carry the ids alone.
type Names struct {
	Org *OrgName
	// Clusters is each cluster's name by its id. It may be shared between requests, so nothing here changes it.
	Clusters map[string]string
	// Unavailable says the server knows the names but could not read them now. A fused read says so once in its warnings;
	// the other answers just carry the ids, since a warning on every list would be noise about something that is a help.
	Unavailable bool
}

// Namer is what an Extras that knows the names of the organisation and its clusters adds to it. It is asked once per
// request, so it is meant to answer from what the server already holds rather than read a store each time.
type Namer interface {
	Names(ctx context.Context) (Names, error)
}

// cluster is the name of a cluster the Scope may see, or "" when it may not or the name is not known. Only visible
// clusters are ever named: a token limited to some clusters must not learn what the others are called.
func (n *Names) cluster(s Scope, id string) string {
	if n == nil || id == "" || !s.ClusterVisible(id) {
		return ""
	}
	return n.Clusters[id]
}

// NameClusters gives each of the trace's resources the name of its cluster, where ikhnos knows it and the Scope may see it.
func (t *Trace) NameClusters(s Scope, n *Names) {
	for _, r := range t.Resources {
		r.ClusterName = n.cluster(s, r.Cluster)
	}
}

// ClustersOf is the dictionary of the clusters the lines come from: each visible cluster once, by id, with its name when
// ikhnos knows it. It is nil when the names are not known at all, since a list of the ids alone would only repeat the lines.
func ClustersOf(s Scope, lines []LogEntry, n *Names) []ClusterName {
	if n == nil || n.Unavailable {
		return nil
	}
	ids := map[string]bool{}
	for _, l := range lines {
		ids[l.Cluster] = true
	}
	return clusterList(s, ids, n)
}

// clusterList is the visible clusters among ids, sorted by id, each named when the name is known.
func clusterList(s Scope, ids map[string]bool, n *Names) []ClusterName {
	var out []ClusterName
	for id := range ids {
		if id != "" && s.ClusterVisible(id) {
			out = append(out, ClusterName{ID: id, Name: n.cluster(s, id)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FusedContext names what a fused trace is part of, so that a reader of the answer need not look the ids up elsewhere: the
// organisation, the clusters its resources ran in and its log lines came from, and the ikhnos applications its resources
// are part of. Only what the caller may see is in it.
type FusedContext struct {
	Org          *OrgName      `json:"org,omitempty"`
	Clusters     []ClusterName `json:"clusters,omitempty"`
	Applications []AppRef      `json:"applications,omitempty"`
}

// fusedContext works out the context of a finished fused read; nil when there is nothing to say. The applications are those
// the resources were tagged with, which come from the groups the caller may see (FuseOptions.Applications).
func fusedContext(f *Fused, s Scope, n *Names) *FusedContext {
	var fc FusedContext
	if n != nil && n.Org != nil && n.Org.Name != "" {
		fc.Org = n.Org
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	for _, r := range f.Resources {
		ids[r.Cluster] = true
		for _, a := range r.Applications {
			if !seen[a.ID] {
				seen[a.ID] = true
				fc.Applications = append(fc.Applications, a)
			}
		}
		for _, l := range r.Logs {
			ids[l.Cluster] = true
		}
	}
	for _, l := range f.Logs.Unmatched {
		ids[l.Cluster] = true
	}
	for _, sp := range f.Spans {
		for _, l := range sp.Logs {
			ids[l.Cluster] = true
		}
	}
	if f.SystemLogs != nil {
		for _, l := range f.SystemLogs.Entries {
			ids[l.Cluster] = true
		}
	}
	fc.Clusters = clusterList(s, ids, n)
	sort.SliceStable(fc.Applications, func(i, j int) bool { return fc.Applications[i].Name < fc.Applications[j].Name })
	if fc.Org == nil && len(fc.Clusters) == 0 && len(fc.Applications) == 0 {
		return nil
	}
	return &fc
}

// nameFused puts the names on a finished fused read: each resource its cluster's, and the answer its context.
func nameFused(f *Fused, s Scope, n *Names) {
	f.Trace.NameClusters(s, n)
	f.Context = fusedContext(f, s, n)
	if n != nil && n.Unavailable && !slices.Contains(f.Warnings, namesUnavailable) {
		f.Warnings = append(f.Warnings, namesUnavailable)
	}
}

// namesUnavailable is the one warning a fused read carries when the names could not be read.
const namesUnavailable = "names: the names of the organisation and its clusters could not be read now, " +
	"so the answer carries their ids alone"
