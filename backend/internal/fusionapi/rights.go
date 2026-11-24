package fusionapi

import (
	"fmt"
	"slices"
)

// A caller whose rights are some ikhnos applications.
//
// A FUSION token may name applications (by id) besides its signals, namespaces and clusters. When it is made, the members
// those applications have at that moment are recorded with it (PinnedMember: each by its id, its name, its kind, and the
// namespace and cluster it runs in), and an administrator approves that list. It then may see only what those members send,
// each in the namespace and cluster it was pinned at: a member's name in another namespace (a redis of another team), another
// service of the same namespace, and telemetry that does not say its namespace and cluster are not its to see. At every
// request the server checks the pinned members against what ikhnos knows now (LimitToApplications): a member that has left
// its application, moved, been renamed or changed kind gives nothing, and one that has joined since gives nothing either,
// however it joined (a person, or its own labels). Rights can so only shrink: to widen a token, make a new one. In this
// package the result is Scope.RightsPlaces, which rights() turns into the exact places every query reads and every answer is
// checked against (places.go); the checks of each kind of answer are below.

// MaxTokenApplications is how many applications one token may be limited to.
const MaxTokenApplications = 20

// MaxTokenPlaces is how many places (a namespace of a cluster) a token's pinned members may run in. Every query of such a
// token names each place (exactly: the union of places is never read for it), so this bounds what one read asks of a store,
// as maxFocusPlaces does for a focus.
const MaxTokenPlaces = 100

// MaxTokenMembers is how many members one token may be pinned to.
const MaxTokenMembers = maxAppServices

// PinnedMember is one member of an application as a token limited to it was made with: the application's id, and the
// member's id, name, kind, namespace and cluster at that moment.
type PinnedMember struct {
	Application string `json:"application"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Namespace   string `json:"namespace"`
	Cluster     string `json:"cluster"`
}

// pin is the member as a token pins it.
func pin(app string, m AppMember) PinnedMember {
	return PinnedMember{Application: app, ID: m.ID, Name: m.Name, Kind: m.Kind, Namespace: m.Namespace, Cluster: m.Cluster}
}

// LimitToApplications is s limited to the applications of these ids and to the members the token was pinned to (pinned):
// a pinned member gives its place and its name only while the application, as groups has it now (every application of the
// organisation, as ikhnos knows them), still has a member of that id with the same name, kind, namespace and cluster, and
// only within s's namespaces and clusters. An edit of an application, or a service that joins it by its labels, can so only
// take members away. A member's name is matched as service_name, and as a workload's name only under the label of its own
// kind; its app labels (Aliases) never count, being its workload's to set. An application that is not in groups gives
// nothing. With none left the Scope sees nothing. RightsServiceIDs are the members' ids, which the topology is matched by.
//
// The notes are for the caller, in /status: which applications are gone and which give it no service. They name only the
// token's own applications and never count what lies outside its limits.
func (s Scope) LimitToApplications(groups []AppGroup, ids []string, pinned []PinnedMember) (Scope, []string) {
	return s.limitTo(groups, ids, func(app string, m AppMember) bool { return slices.Contains(pinned, pin(app, m)) })
}

// ApplicationMembers are the members the applications of these ids give a token with s's namespaces and clusters now, as
// LimitToApplications counts them: those with an id, a name, a namespace and a cluster, within the limits. It is what a new
// token is pinned to (what an administrator is shown to approve), in the order of the applications and of their members.
func (s Scope) ApplicationMembers(groups []AppGroup, ids []string) []PinnedMember {
	var out []PinnedMember
	s.limitTo(groups, ids, func(app string, m AppMember) bool {
		out = append(out, pin(app, m))
		return true
	})
	return out
}

func (s Scope) limitTo(groups []AppGroup, ids []string, pinned func(app string, m AppMember) bool) (Scope, []string) {
	out := s
	out.AppLimited, out.RightsApps, out.RightsPlaces, out.RightsServiceIDs = true, nil, nil, nil
	byID := map[string]*AppGroup{}
	for i := range groups {
		byID[groups[i].ID] = &groups[i]
	}
	idx := map[[2]string]int{}
	var notes []string
	for _, id := range ids {
		g := byID[id]
		if g == nil {
			notes = append(notes, fmt.Sprintf("application %s no longer exists in ikhnos, so it gives this token nothing", id))
			continue
		}
		if slices.ContainsFunc(out.RightsApps, func(a AppRef) bool { return a.ID == id }) {
			continue
		}
		out.RightsApps = append(out.RightsApps, g.Ref())
		placed := false
		for _, m := range g.Members {
			if m.ID == "" || m.Name == "" || m.Namespace == "" || m.Cluster == "" || !within(s.Namespaces, m.Namespace) ||
				!within(s.Clusters, m.Cluster) || !pinned(id, m) {
				continue
			}
			k := [2]string{m.Namespace, m.Cluster}
			i, ok := idx[k]
			if !ok {
				i = len(out.RightsPlaces)
				idx[k] = i
				out.RightsPlaces = append(out.RightsPlaces, FocusPlace{Namespace: m.Namespace, Cluster: m.Cluster, Workloads: map[string][]string{}})
			}
			f := &out.RightsPlaces[i]
			f.Names = append(f.Names, m.Name)
			if w := workloadLabel[m.Kind]; w != "" {
				f.Workloads[w] = append(f.Workloads[w], m.Name)
			}
			out.RightsServiceIDs = append(out.RightsServiceIDs, m.ID)
			placed = true
		}
		if !placed {
			notes = append(notes, fmt.Sprintf("application %s (%s) has none of the services this token was made with any more, "+
				"where they ran then: they left it, moved or were renamed in ikhnos", g.Name, g.ID))
		}
	}
	for i := range out.RightsPlaces {
		f := &out.RightsPlaces[i]
		f.Names = distinctSorted(f.Names)
		for w := range f.Workloads {
			f.Workloads[w] = distinctSorted(f.Workloads[w])
		}
	}
	out.RightsServiceIDs = distinctSorted(out.RightsServiceIDs)
	SortByPlace(out.RightsPlaces, func(p FocusPlace) (string, string) { return p.Cluster, p.Namespace })
	if len(out.RightsPlaces) == 0 {
		notes = append(notes, SeesNothing)
	}
	return out, notes
}

// ownsApp says whether the application of this id is one the Scope's rights are limited to.
func (s Scope) ownsApp(id string) bool {
	return slices.ContainsFunc(s.RightsApps, func(a AppRef) bool { return a.ID == id })
}

// resourceVisible is the second check on a resource (of a span, a trace): for a caller limited to applications, its
// namespace and cluster are those of a place, and its service.name is one of the place's names, or the name of its pod's
// workload is, under the attribute of the member's own kind (a Deployment's name as k8s.deployment.name, never as another
// kind's). Only a whole place counts: a resource that does not say its namespace or cluster is no member's. Otherwise its
// namespace and cluster are ones the rights may see.
func (s Scope) resourceVisible(attrs map[string]any) bool {
	ns, cl := strAttr(attrs, attrNamespace), strAttr(attrs, attrCluster)
	p := s.rights()
	if !p.named() {
		return p.contains(ns, cl, "")
	}
	if n := strAttr(attrs, attrService); n != "" && p.contains(ns, cl, n) {
		return true
	}
	for label, attr := range workloadAttrOf {
		if n := strAttr(attrs, attr); n != "" && p.containsWorkload(ns, cl, label, n) {
			return true
		}
	}
	return false
}

// workloadAttrOf is the resource attribute of each workload label.
var workloadAttrOf = map[string]string{vocDeployment.Name(): vocDeployment.Attr, vocStatefulSet.Name(): vocStatefulSet.Attr, vocDaemonSet.Name(): vocDaemonSet.Attr}

// seriesVisible is the second check on a series by its labels: its namespace and cluster, and for a caller limited to
// applications its service_name, or one of the workload labels a metrics read matches the names by (MetricFilter.selectors)
// holding the name of a member of that kind.
func (s Scope) seriesVisible(l map[string]string) bool {
	ns, cl := l[lblNamespace], l[lblCluster]
	p := s.rights()
	if !p.named() {
		return p.contains(ns, cl, "")
	}
	if n := l[lblService]; n != "" && p.contains(ns, cl, n) {
		return true
	}
	for _, w := range lblWorkloads {
		if n := l[w]; n != "" && p.containsWorkload(ns, cl, w, n) {
			return true
		}
	}
	return false
}

// lineVisible is the second check on a log line: its stream's namespace and cluster (see lokiEntry), and for a caller
// limited to applications its stream's service_name.
func (s Scope) lineVisible(e LogEntry) bool { return s.visibleNamed(e.Namespace, e.Cluster, e.Service) }

// serviceVisible is the second check on a service of ikhnos' topology: its namespace and cluster. For a caller limited to
// applications, the service is a member of one of them by its id (RightsServiceIDs), at a place of the rights under its own
// name: a service that merely shares a member's name, or one of its app labels, is not.
func (s Scope) serviceVisible(sv *TopoService) bool {
	if sv == nil {
		return false
	}
	if !s.AppLimited {
		return s.visibleAt(sv.Namespace, sv.Cluster)
	}
	return slices.Contains(s.RightsServiceIDs, sv.ID) && s.rights().contains(sv.Namespace, sv.Cluster, sv.Name)
}

// ServiceVisible is serviceVisible for the server's own lists of where services run.
func (s Scope) ServiceVisible(sv *TopoService) bool { return s.serviceVisible(sv) }

// memberVisible is whether the caller may see a member of an application: where it runs, and for a caller limited to
// applications only a member that gives it rights (by its id, at its place).
func (s Scope) memberVisible(m AppMember) bool {
	if !s.AppLimited {
		return s.visibleAt(m.Namespace, m.Cluster)
	}
	return m.ID != "" && slices.Contains(s.RightsServiceIDs, m.ID) && s.rights().contains(m.Namespace, m.Cluster, m.Name)
}

// ownedService is the service as an answer to the caller gives it: for a caller limited to applications, a copy that names
// only the caller's own applications among those the service is in (the others are not the caller's to know of).
func (s Scope) ownedService(sv *TopoService) *TopoService {
	if sv == nil || !s.AppLimited {
		return sv
	}
	c := *sv
	c.Applications, c.AppIDs = s.ownedApps(sv)
	return &c
}

// ownedApps are the names and ids of the service's applications that are the caller's own (for a caller limited to them).
func (s Scope) ownedApps(sv *TopoService) (names, ids []string) {
	for i, id := range sv.AppIDs {
		if s.ownsApp(id) && i < len(sv.Applications) {
			names, ids = append(names, sv.Applications[i]), append(ids, id)
		}
	}
	return names, ids
}

// intersectPlaces is the part of the places of an application (focus, from AppGroup.places, where a place with no namespace
// or cluster takes any) inside the exact places of the rights: each place of the rights the focus reaches, with the names
// both have there.
func intersectPlaces(focus, rights []FocusPlace) []FocusPlace {
	idx := map[[2]string]int{}
	var out []FocusPlace
	for _, f := range focus {
		for _, r := range rights {
			if (f.Namespace != "" && f.Namespace != r.Namespace) || (f.Cluster != "" && f.Cluster != r.Cluster) {
				continue
			}
			var names []string
			for _, n := range f.Names {
				if slices.Contains(r.Names, n) {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				continue
			}
			k := [2]string{r.Namespace, r.Cluster}
			i, ok := idx[k]
			if !ok {
				i = len(out)
				idx[k] = i
				out = append(out, FocusPlace{Namespace: r.Namespace, Cluster: r.Cluster})
				if r.Workloads != nil {
					out[i].Workloads = map[string][]string{}
				}
			}
			out[i].Names = append(out[i].Names, names...)
			for w, ws := range r.Workloads { // a name keeps the kind the rights give it
				for _, n := range ws {
					if slices.Contains(f.Names, n) {
						out[i].Workloads[w] = append(out[i].Workloads[w], n)
					}
				}
			}
		}
	}
	for i := range out {
		out[i].Names = distinctSorted(out[i].Names)
		for w := range out[i].Workloads {
			out[i].Workloads[w] = distinctSorted(out[i].Workloads[w])
		}
	}
	SortByPlace(out, func(p FocusPlace) (string, string) { return p.Cluster, p.Namespace })
	return out
}

// focusInRights is FocusOn for a caller limited to applications: the application's places inside the caller's, exact, and
// their union as the cut. An application none of whose places the caller has is "not found", as one that does not exist.
func (s Scope) focusInRights(g *AppGroup, what string) (Scope, error) {
	places := intersectPlaces(g.places(), s.rights().exact)
	if len(places) == 0 {
		return Scope{}, errf(404, "no %s %q", what, g.Name)
	}
	var names, nss, cls []string
	for _, p := range places {
		names, nss, cls = append(names, p.Names...), append(nss, p.Namespace), append(cls, p.Cluster)
	}
	out := s
	out.FocusServices, out.FocusNamespaces, out.FocusClusters = distinctSorted(names), distinctSorted(nss), distinctSorted(cls)
	if len(out.FocusServices) > maxAppServices {
		return Scope{}, badRequest("%s %q has %d service names, more than the %d one filter can carry", what, g.Name, len(out.FocusServices), maxAppServices)
	}
	out.Focus = places // (always exact, however many: the rights are never read by their union)
	return out, nil
}
