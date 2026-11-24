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

// selector is the stream selector alone: the part Loki's label-value and series calls accept. The places' names and
// namespaces are in it (placeSet.cutOn), each after the filter's own, so that Loki's index serves them.
func (f LogFilter) selector(s Scope) (string, error) {
	nsCut, _, namesCut := s.places().cutOn(labelSyntax)
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
	case namesCut == "":
		// A selector needs a matcher that cannot be empty, and service_name=~".+" leaves no stream out: Loki (3.x; the
		// chart deploys 3.5) gives every stream a service_name as it stores it. A record with no service.name takes the
		// first label of limits_config.discover_service_name that it has (service, app, ..., k8s_container_name, ...),
		// else "unknown_service" (pkg/loghttp/push/otlp.go for OTLP, push.go for the rest). That list is on by default
		// and the chart's loki-config.yaml leaves it so; set to an empty list it would hide such lines here.
		sel = append(sel, lblService+`=~".+"`)
	}
	if namesCut != "" {
		sel = append(sel, namesCut)
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
	if nsCut != "" {
		sel = append(sel, nsCut)
	}
	sel = append(sel, logCategoryMatchers(lblNamespace, f.Categories)...)
	return "{" + strings.Join(sel, ",") + "}", nil
}

// pipeline adds the line and label filters to a selector. The places' clusters are a label filter here, not a stream
// matcher: lines stored before the cluster became an index label carry it as structured metadata only. An exact focus is
// one more, an "or" of the places (a stream selector has no "or").
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
		// (Loki 3.5.0 writes detected_level as structured metadata at ingestion, OTLP pushes included, when
		// discover_log_levels and allow_structured_metadata are on, as the FUSION chart's loki-config.yaml sets them; see
		// the comment there for what was checked in its source. severity_text is structured metadata of every OTLP record
		// that has one, so both are read by a label filter after the pipe, and either is "" where there is none.)
		q += " | (" + lokiSeverity + "=~" + quote(re) + " or (" + lokiSeverity + `="" and (detected_level=~` + quote(re) + " or level=~" + quote(re) + ")))"
	}
	if f.Cluster != "" {
		if err := checkValue("cluster", f.Cluster); err != nil {
			return "", err
		}
		q += " | " + lblCluster + "=" + quote(f.Cluster)
	}
	p := s.places()
	if _, clCut, _ := p.cutOn(labelSyntax); clCut != "" {
		q += " | " + clCut
	}
	if len(p.exact) > 0 {
		// The selector takes the union of the application's names, namespaces and clusters (what Loki's index can serve); this
		// keeps a line only where its service is a member in that member's own namespace and cluster.
		q += " | " + labelSyntax.either(p.placeConds(labelSyntax))
	}
	return q, nil
}
