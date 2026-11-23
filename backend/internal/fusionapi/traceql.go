package fusionapi

import (
	"slices"
	"strings"
	"time"
)

// TraceFilter picks traces. Every field is optional and combines with AND; the conditions are about spans, so a trace
// matches when some span of it does.
type TraceFilter struct {
	Service   string `json:"service,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	// Pod and Node keep traces with a span of a resource on this pod, or this node.
	Pod        string   `json:"pod,omitempty"`
	Node       string   `json:"node,omitempty"`
	Categories []string `json:"categories,omitempty"` // kubernetes or application, by namespace (see traceCategoryCond)
	Name       string   `json:"name,omitempty"`       // span name
	Status     string   `json:"status,omitempty"`     // error | ok | unset
	// MinDuration and MaxDuration bound the length of the trace; for a caller whose Scope is limited, of a span the caller may see.
	MinDuration time.Duration `json:"minDuration,omitempty"`
	MaxDuration time.Duration `json:"maxDuration,omitempty"`
}

func (f TraceFilter) traceQL(s Scope) (string, error) {
	c, err := eqMatchers([]eqFilter{
		{"service", "resource." + attrService, f.Service}, {"namespace", "resource." + attrNamespace, f.Namespace},
		{"cluster", "resource." + attrCluster, f.Cluster}, {"pod", "resource." + attrPod, f.Pod}, {"node", "resource." + attrNode, f.Node},
		{"name", "name", f.Name},
	}, func(l, v string) string { return l + " = " + v })
	if err != nil {
		return "", err
	}
	switch f.Status {
	case "":
	case "error", "ok", "unset":
		c = append(c, "status = "+f.Status)
	default:
		return "", badRequest("status must be error, ok or unset")
	}
	// The length of a whole trace covers spans a limited caller may not see, and a threshold moved up and down would measure them.
	// A limited caller's threshold is on the spans it may see (a span at least this long), which is also all it is told the extent of.
	dur := "traceDuration"
	if !s.Unrestricted() {
		dur = "duration"
	}
	if f.MinDuration > 0 {
		c = append(c, dur+" >= "+f.MinDuration.String())
	}
	if f.MaxDuration > 0 {
		c = append(c, dur+" <= "+f.MaxDuration.String())
	}
	cat, err := traceCategoryCond("resource."+attrNamespace, f.Categories)
	if err != nil {
		return "", err
	}
	if cat != "" {
		c = append(c, cat)
	}
	p := s.places()
	c = append(c, p.cutConds(traceSyntax)...)
	if len(p.exact) > 0 { // exact: a member's name only where that member runs
		c = append(c, "("+traceSyntax.either(p.placeConds(traceSyntax))+")")
	}
	if len(c) == 0 {
		return "{ true }", nil
	}
	return "{ " + strings.Join(c, " && ") + " }", nil
}

// anyOf is the TraceQL condition that attr is one of vals: attr = "v" for one value, else one regular expression of the
// values, each escaped (regexAny), attr =~ "^(?:a|b|c)$". One expression instead of an "or" of each keeps the query
// short: the cut and the exact places of an application can list hundreds of names.
//
// It must match the listed values and nothing more, or a caller's rights would let through a namespace that merely
// contains an allowed one. Tempo anchors a TraceQL regular expression at both ends since 2.7 (#4329: "All regular
// expression matchers will now be fully anchored"; it is compiled by Prometheus' labels.NewFastRegexMatcher as
// ^(?s:...)$), and the chart deploys 2.8; before that it matched anywhere in the value. The expression carries its own
// anchors so that it is exact on either, and they cost nothing on the newer: the matcher drops them before turning the
// alternation into a set lookup. A list with the empty value keeps the "or" of each: an empty attribute and a missing
// one are told apart by =, and a regular expression that matches "" is not promised to keep them apart.
func anyOf(attr string, vals []string) string {
	if len(vals) == 1 {
		return attr + " = " + quote(vals[0])
	}
	if !slices.Contains(vals, "") {
		return attr + " =~ " + quote("^(?:"+regexAny(vals)+")$")
	}
	p := make([]string, len(vals))
	for i, v := range vals {
		p[i] = attr + " = " + quote(v)
	}
	return "(" + strings.Join(p, " || ") + ")"
}
