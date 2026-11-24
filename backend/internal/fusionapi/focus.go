package fusionapi

import (
	"fmt"
	"slices"
	"sort"
)

// A fused read narrowed to one ikhnos application.
//
// The trace itself is what it is: every span stays, whatever service wrote it, because a request that leaves the
// application and comes back is still one request. What is narrowed is what the read joins to it: the trace's log lines,
// each resource's metrics and context lines, the system lines of its nodes and the caller's own per-resource PromQL are
// read for the application's resources only. Every resource also says which applications it is part of (Resource.
// Applications), whether or not the read was narrowed, so a client can tell the two parts of a trace apart.

// AppRef names an ikhnos application.
type AppRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Ref is the application's id and name.
func (g *AppGroup) Ref() AppRef { return AppRef{ID: g.ID, Name: g.Name} }

// workloadAttrs are the resource attributes that name the workload a pod belongs to (set by the cluster's k8sattributes),
// in the order a resource's workload is taken from.
var workloadAttrs = []string{"k8s.deployment.name", "k8s.statefulset.name", "k8s.daemonset.name", "k8s.job.name", "k8s.cronjob.name"}

// Workload is the name of the workload the resource's pod belongs to, or "" when its attributes do not say.
func (r *Resource) Workload() string {
	for _, a := range workloadAttrs {
		if v := strAttr(r.Attributes, a); v != "" {
			return v
		}
	}
	return ""
}

// match says how a member is matched with a resource of a trace.
type match int

const (
	noMatch match = iota
	// byName: the resource has the member's name, but does not say which namespace or cluster it is in, so the member's
	// cannot be checked.
	byName
	byPlace // the name, and every namespace and cluster both sides give
)

// matchResource matches a member with a resource: by its name or one of its aliases against the resource's service.name
// or the name of any workload its pod belongs to (a CronJob's run carries the CronJob's name as well as its Job's, which
// Workload gives first), and by its namespace and cluster where both say one.
func (m AppMember) matchResource(r *Resource) match {
	names := append([]string{m.Name}, m.Aliases...)
	if !slices.Contains(names, r.Service) && !slices.ContainsFunc(workloadAttrs, func(a string) bool {
		w := strAttr(r.Attributes, a)
		return w != "" && slices.Contains(names, w)
	}) {
		return noMatch
	}
	loose := false
	for _, d := range [][2]string{{m.Namespace, r.Namespace}, {m.Cluster, r.Cluster}} {
		switch {
		case d[0] == "":
		case d[1] == "":
			loose = true
		case d[0] != d[1]:
			return noMatch
		}
	}
	if loose {
		return byName
	}
	return byPlace
}

// byWorkloadOnly says whether the resource is part of the application by its workload's name alone: none of the
// application's names (a member's, or an alias) is its service.name. Its series then carry a service_name that is not
// the application's, or none, and a query narrowed to the application by service_name (narrowToApplication) does not
// read them.
func (g *AppGroup) byWorkloadOnly(r *Resource) bool {
	return g.matchResource(r) != noMatch && !slices.Contains(g.ServiceNames(), r.Service)
}

// matchResource is the best match of any member of the application.
func (g *AppGroup) matchResource(r *Resource) match {
	best := noMatch
	for _, m := range g.Members {
		best = max(best, m.matchResource(r))
	}
	return best
}

// tagApplications sets Resource.Applications from the applications the caller may know of.
func tagApplications(resources []*Resource, groups []AppGroup) {
	for _, r := range resources {
		r.Applications = nil
		for i := range groups {
			if groups[i].matchResource(r) != noMatch {
				r.Applications = append(r.Applications, groups[i].Ref())
			}
		}
		sort.Slice(r.Applications, func(a, b int) bool { return r.Applications[a].Name < r.Applications[b].Name })
	}
}

// focusResources keeps the resources that are part of the application, and says in warnings what was left out and what
// was kept on its name alone.
func focusResources(g *AppGroup, resources []*Resource) (in []*Resource, warns []string) {
	var out, loose []string
	for _, r := range resources {
		switch g.matchResource(r) {
		case noMatch:
			out = append(out, r.Key)
		case byName:
			loose = append(loose, r.Key)
			in = append(in, r)
		default:
			in = append(in, r)
		}
	}
	if len(in) == 0 {
		warns = append(warns, fmt.Sprintf("application %s: none of the trace's resources is part of it, so nothing is joined to the trace", g.Name))
	} else if len(out) > 0 {
		warns = append(warns, fmt.Sprintf("application %s: %d of the trace's resources are not part of it (%s); their logs, metrics and queries are not read", g.Name, len(out), names(out)))
	}
	if len(loose) > 0 {
		warns = append(warns, fmt.Sprintf("application %s: %d resource(s) are taken as part of it by name alone, because they do not say their namespace or cluster: %s", g.Name, len(loose), names(loose)))
	}
	return in, warns
}

// servicesOf is the distinct service names of the resources, sorted.
func servicesOf(resources []*Resource) []string {
	var out []string
	for _, r := range resources {
		if r.Service != "" {
			out = append(out, r.Service)
		}
	}
	return distinctSorted(out)
}
