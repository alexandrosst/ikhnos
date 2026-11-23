package fusionapi

import (
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// PromQL written by a caller whose access is limited to certain namespaces or clusters.
//
// Such a query cannot be checked by looking at it, nor safely wrapped (an outer "and on (namespace)" is undone by a label_replace
// inside), so it is parsed and the caller's limits are added as matchers to every selector in it: every series the query reads,
// in a range, a subquery or an instant vector, is one the caller may see, whatever the query then does with it. What comes
// out (a sum without the namespace, a label renamed) is computed from those series alone, so it is not checked again by its
// labels the way a structured read's results are. The same holds for the queries of a fused read.

// restrictPromQL is expr with the Scope's namespace and cluster limits on every selector, as Prometheus would print it. A
// Scope with no limit gets expr back as written.
func restrictPromQL(expr string, s Scope) (string, error) {
	if s.Unrestricted() {
		return expr, nil
	}
	return withMatchers(expr, map[string][]string{lblNamespace: s.Namespaces, lblCluster: s.Clusters})
}

// narrowToApplication is expr with the application's own on every selector: its service names (a member's name and aliases,
// as service_name), and its namespaces and clusters when every member has one. It is the union of the members, so for an
// application whose members run in several places a name may be matched in another member's namespace; a series with no
// service_name (a pod's, from an agent that does not name it after its workload) is not matched.
func narrowToApplication(expr string, g *AppGroup) (string, error) {
	lim := map[string][]string{lblService: g.ServiceNames()}
	all := func(of func(AppMember) string) bool {
		for _, m := range g.Members {
			if of(m) == "" {
				return false
			}
		}
		return true
	}
	if all(func(m AppMember) string { return m.Namespace }) {
		lim[lblNamespace] = g.Namespaces()
	}
	if all(func(m AppMember) string { return m.Cluster }) {
		lim[lblCluster] = g.Clusters()
	}
	return withMatchers(expr, lim)
}

// withMatchers parses expr and adds, to every selector in it, a matcher per label for any of its values (labels with no value
// are left alone).
func withMatchers(expr string, lim map[string][]string) (string, error) {
	e, err := parser.ParseExpr(expr)
	if err != nil {
		return "", badRequest("not a PromQL query this API can read: %s", strings.TrimSpace(firstLine(err.Error())))
	}
	var add []*labels.Matcher
	for _, l := range []string{lblService, lblNamespace, lblCluster} { // a fixed order, so the query reads the same each time
		if len(lim[l]) == 0 {
			continue
		}
		m, err := labels.NewMatcher(labels.MatchRegexp, l, regexAny(lim[l]))
		if err != nil {
			return "", err
		}
		add = append(add, m)
	}
	var refused error
	parser.Inspect(e, func(n parser.Node, _ []parser.Node) error {
		switch x := n.(type) {
		case *parser.VectorSelector:
			x.LabelMatchers = append(x.LabelMatchers, add...)
		case *parser.Call:
			// info() reads the info series it joins on itself, past the selectors of the query.
			if x.Func != nil && x.Func.Name == "info" && refused == nil {
				refused = badRequest("info() reads series of its own, so it is only for a caller whose access is not limited")
			}
		}
		return nil
	})
	if refused != nil {
		return "", refused
	}
	return e.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
