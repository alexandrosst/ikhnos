package fusionapi

import (
	"sort"

	"ikhnos/internal/model"
)

// AppGroup is an ikhnos application: a named group of services. What FUSION calls an "application" elsewhere is a single
// service.name; this is the grouping a person made in ikhnos, which telemetry itself does not carry.
type AppGroup struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Members     []AppMember `json:"services"`
	// Origin and Confidence say how the application came to be (a person, or discovery).
	Origin, Confidence string `json:"-"`
	// Unresolved counts the members ikhnos has an id for but cannot tie to a service (a cluster that is not connected, a
	// service that has gone): they cannot be matched with telemetry, so they are not in Members.
	Unresolved int `json:"-"`
}

// AppMember is one service of an application, by the identity telemetry carries.
type AppMember struct {
	// ID is the service's id in ikhnos (the topology's, or the record's of a service a person wrote in by hand).
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	Kind      string `json:"kind,omitempty"`
	// Aliases are other names the telemetry of this service may carry as its service.name (its app label), besides Name.
	Aliases []string `json:"aliases,omitempty"`
}

// Key is the identity telemetry knows the service by.
func (m AppMember) Key() model.ServiceKey {
	return model.ServiceKey{Cluster: m.Cluster, Namespace: m.Namespace, Name: m.Name}
}

// ServiceNames is every service.name telemetry of this application may carry: each member's name and aliases, sorted and without repeats.
func (g AppGroup) ServiceNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range g.Members {
		for _, n := range append([]string{m.Name}, m.Aliases...) {
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Namespaces and Clusters are those the members run in, sorted and without repeats.
func (g AppGroup) Namespaces() []string {
	return g.distinct(func(m AppMember) string { return m.Namespace })
}

func (g AppGroup) Clusters() []string {
	return g.distinct(func(m AppMember) string { return m.Cluster })
}

func (g AppGroup) distinct(f func(AppMember) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range g.Members {
		if v := f(m); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
