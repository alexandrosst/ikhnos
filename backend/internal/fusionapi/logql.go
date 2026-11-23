package fusionapi

import (
	"regexp"
	"strings"
)

var severityText = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// severityRegex turns "error" or "error,warn" into a case-insensitive match for any of them.
func severityRegex(v string) (string, error) {
	parts := strings.Split(v, ",")
	if len(parts) > 8 {
		return "", badRequest("severity names at most 8 levels")
	}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if !severityText.MatchString(p) {
			return "", badRequest("severity must be words such as error, warn or info, separated by commas")
		}
		parts[i] = p
	}
	if len(parts) == 1 {
		return "(?i)" + parts[0], nil // (Loki anchors a label regex itself)
	}
	return "(?i)(" + strings.Join(parts, "|") + ")", nil
}

// logQL builds the query: a stream selector (always at least one matcher that cannot be empty, which Loki requires),
// then line filters, then label filters on the structured metadata. The Scope's namespace limit goes in the selector
// and its cluster limit in the filters.
func (f LogFilter) logQL(s Scope) (string, error) {
	sel, err := f.selector(s)
	if err != nil {
		return "", err
	}
	return f.pipeline(s, sel)
}

// selector is the stream selector alone: the part Loki's label-value and series calls accept.
func (f LogFilter) selector(s Scope) (string, error) {
	var sel []string
	switch {
	case f.Service != "":
		if err := checkValue("service", f.Service); err != nil {
			return "", err
		}
		sel = append(sel, lblService+"="+quote(f.Service))
	case len(f.Services) > 0:
		for _, n := range f.Services {
			if err := checkValue("service", n); err != nil {
				return "", err
			}
		}
		sel = append(sel, lblService+"=~"+quote(regexAny(f.Services)))
	case len(s.FocusServices) == 0:
		sel = append(sel, lblService+`=~".+"`)
	}
	if len(s.FocusServices) > 0 {
		sel = append(sel, lblService+"=~"+quote(regexAny(s.FocusServices)))
	}
	eq, err := eqMatchers([]eqFilter{{"namespace", lblNamespace, f.Namespace}, {"pod", lblPod, f.Pod}}, func(l, v string) string { return l + "=" + v })
	if err != nil {
		return "", err
	}
	sel = append(sel, eq...)
	if len(f.pods) > 0 {
		for _, p := range f.pods {
			if err := checkValue("pod", p); err != nil {
				return "", err
			}
		}
		sel = append(sel, lblPod+"=~"+quote(regexAny(f.pods)))
	}
	if len(f.Namespaces) > 0 {
		for _, n := range f.Namespaces {
			if err := checkValue("namespace", n); err != nil {
				return "", err
			}
		}
		sel = append(sel, lblNamespace+"=~"+quote(regexAny(f.Namespaces)))
	}
	if ns := s.nsLimit(); len(ns) > 0 {
		sel = append(sel, lblNamespace+"=~"+quote(regexAny(ns)))
	}
	sel = append(sel, logCategoryMatchers(lblNamespace, f.Categories)...)
	return "{" + strings.Join(sel, ",") + "}", nil
}

// pipeline adds the line and label filters to a selector.
func (f LogFilter) pipeline(s Scope, q string) (string, error) {
	if f.Contains != "" {
		if err := checkValue("contains", f.Contains); err != nil {
			return "", err
		}
		q += " |= " + quote(f.Contains)
	}
	if f.TraceID != "" {
		id, err := NormalizeTraceID(f.TraceID)
		if err != nil {
			return "", err
		}
		q += " | " + lokiTraceID + "=" + quote(id)
	}
	if f.SpanID != "" {
		id, err := NormalizeSpanID(f.SpanID)
		if err != nil {
			return "", err
		}
		q += " | " + lokiSpanID + "=" + quote(id)
	}
	if f.NoTrace {
		q += " | " + lokiTraceID + `=""`
	}
	if f.Node != "" {
		if err := checkValue("node", f.Node); err != nil {
			return "", err
		}
		q += " | " + lblNode + "=" + quote(f.Node)
	}
	if f.Severity != "" {
		re, err := severityRegex(f.Severity)
		if err != nil {
			return "", err
		}
		// The severity a line is shown with: the record's own (severity_text), else what Loki detected from the line or a level
		// label - a container's stdout has no severity_text, and would otherwise be shown as "error" and never found as one.
		q += " | (" + lokiSeverity + "=~" + quote(re) + " or (" + lokiSeverity + `="" and (detected_level=~` + quote(re) + " or level=~" + quote(re) + ")))"
	}
	if f.Cluster != "" {
		if err := checkValue("cluster", f.Cluster); err != nil {
			return "", err
		}
		q += " | " + lblCluster + "=" + quote(f.Cluster)
	}
	if cl := s.clLimit(); len(cl) > 0 {
		q += " | " + lblCluster + "=~" + quote(regexAny(cl))
	}
	if len(s.Focus) > 0 {
		// The selector takes the union of the application's names, namespaces and clusters (what Loki's index can serve); this
		// keeps a line only where its service is a member in that member's own namespace and cluster.
		var alts []string
		for _, c := range placeConds(s.Focus, lblNamespace, lblCluster, lblService, func(k, v string) string { return k + "=" + quote(v) },
			func(k string, vs []string) string { return k + "=~" + quote(regexAny(vs)) }) {
			alts = append(alts, "("+strings.Join(c, " and ")+")")
		}
		q += " | " + strings.Join(alts, " or ")
	}
	return q, nil
}
