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
	e, err := parser.ParseExpr(expr)
	if err != nil {
		return "", badRequest("not a PromQL query this API can read: %s", strings.TrimSpace(firstLine(err.Error())))
	}
	var add []*labels.Matcher
	for _, lim := range []struct {
		label string
		vals  []string
	}{{lblNamespace, s.Namespaces}, {lblCluster, s.Clusters}} {
		if len(lim.vals) == 0 {
			continue
		}
		m, err := labels.NewMatcher(labels.MatchRegexp, lim.label, regexAny(lim.vals))
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
