package fusionapi

import (
	"strings"
	"sync"

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
//
// A Scope limited to applications is refused: its rights are places (this name in this namespace and cluster), and the
// matchers of a selector all hold at once, so the most they could carry is the union of the places, which would let a
// member's name be read in another member's namespace; and what such a query computes carries no labels to check again.
func restrictPromQL(expr string, s Scope) (string, error) {
	if s.AppLimited {
		return "", denied("PromQL of your own cannot be sent with this token: %s; use /metrics/range, /metrics/series and the fused reads' metrics instead", AppLimitedRefusal)
	}
	if s.Unrestricted() {
		return expr, nil
	}
	return withMatchers(expr, s.rights().promLimits(), limitedByToken)
}

// Why withMatchers was asked, in the words a refusal uses: the caller's own limits, or a narrowing the caller chose.
const (
	limitedByToken = "so a query from a token whose access is limited to certain namespaces or clusters cannot use it"
	narrowedByRead = "so a query narrowed to an application or to promql_services cannot use it; " +
		"with a token that has no namespace or cluster limit, promql_as_written=true sends the query as written"
)

// narrowToApplication is expr with the application's own on every selector: its service names (a member's name and aliases,
// as service_name), and its namespaces and clusters when every member has one (AppGroup.cut, the cut FocusOn starts from
// too). It is the union of the members, so for an application whose members run in several places a name may be matched in
// another member's namespace; a series with no service_name (a pod's, from an agent that does not name it after its
// workload) is not matched.
//
// The names are matched as service_name only. A metrics read built here also matches them as the workload's name
// (k8s_deployment_name and the like, one selector per label: MetricFilter.selectors), but a selector holds matchers that
// must all hold, with no "or" across labels, and a caller's query cannot be rewritten to read both without changing what
// it computes: v{...} can become (v{service_name=~N} or v{k8s_deployment_name=~N}) only where it is an instant vector. A
// range vector has no "or": rate(v[1m]) would need a subquery, (... or ...)[1m:step], which reads samples at the
// subquery's steps instead of the ones stored. So the query keeps its meaning, the join text says what it matches, and
// fusePromQL warns when a query came back empty for an application some of whose resources in the trace are part of it
// by their workload's name alone (AppGroup.byWorkloadOnly).
func narrowToApplication(expr string, g *AppGroup) (string, error) {
	return withMatchers(expr, g.cut().promLimits(), narrowedByRead)
}

// withMatchers parses expr and adds, to every selector in it, a matcher per label for any of its values (labels with no value
// are left alone). why says, for a refusal, why the query is narrowed (limitedByToken or narrowedByRead).
//
// The functions a narrowed query may call are Prometheus's stable ones. info() reads the info series it joins on
// itself, past the selectors of the query; an experimental function may change what it reads from one Prometheus
// release to the next, so none is taken here, whether or not this process has experimental functions enabled. The query
// is parsed with every function known, so that the refusal says which function and why rather than that it is "not
// enabled".
func withMatchers(expr string, lim map[string][]string, why string) (string, error) {
	p := parser.NewParser(expr, parser.WithFunctions(everyFunction()))
	defer p.Close()
	e, err := p.ParseExpr()
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
			if x.Func == nil || refused != nil {
				break
			}
			if x.Func.Name == "info" {
				refused = badRequest("info() reads series of its own, past the selectors of the query, %s", why)
			} else if f := parser.Functions[x.Func.Name]; f == nil || f.Experimental {
				refused = badRequest("%s() is an experimental PromQL function, and what an experimental function reads may change "+
					"from one Prometheus release to the next, %s", x.Func.Name, why)
			}
		}
		return nil
	})
	if refused != nil {
		return "", refused
	}
	return e.String(), nil
}

// everyFunction is Prometheus's functions with the experimental ones parsed like the rest, so that withMatchers can
// refuse them by name.
var everyFunction = sync.OnceValue(func() map[string]*parser.Function {
	out := make(map[string]*parser.Function, len(parser.Functions))
	for name, f := range parser.Functions {
		c := *f
		c.Experimental = false
		out[name] = &c
	}
	return out
})

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
