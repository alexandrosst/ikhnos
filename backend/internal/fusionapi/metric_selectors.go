package fusionapi

import (
	"regexp"
	"slices"
	"strings"
)

// matchers builds the label matchers of a selector: the filter's own, then the Scope's, each as its own matcher so a
// filter that contradicts the Scope simply matches nothing.
func (f MetricFilter) matchers(s Scope) ([]string, error) {
	var m []string
	switch {
	case f.Name != "":
		if err := checkValue("name", f.Name); err != nil {
			return nil, err
		}
		m = append(m, lblName+"="+quote(f.Name))
	case f.NameRegex != "":
		if err := checkValue("metric", f.NameRegex); err != nil {
			return nil, err
		}
		if _, err := regexp.Compile(f.NameRegex); err != nil {
			return nil, badRequest("metric is not a valid regular expression")
		}
		m = append(m, lblName+"=~"+quote(f.NameRegex))
	default:
		m = append(m, lblName+`=~".+"`)
	}
	if cm := metricNameMatcher(f.Categories); cm != "" {
		m = append(m, cm)
	}
	if !slices.Contains(infoMetrics, f.Name) {
		// The series the server writes to say which services are in which application is not telemetry: it would make every
		// service of an application look like it reports a metric of that name. Nor are those that name the clusters and the
		// organisation for Grafana (ClusterInfoMetric, OrgInfoMetric).
		for _, n := range infoMetrics {
			m = append(m, lblName+"!="+quote(n))
		}
	}
	eq, err := eqMatchers([]eqFilter{
		{"service", lblService, f.Service}, {"namespace", lblNamespace, f.Namespace}, {"pod", lblPod, f.Pod},
		{"node", lblNode, f.Node}, {"cluster", lblCluster, f.Cluster},
	}, func(l, v string) string { return l + "=" + v })
	if err != nil {
		return nil, err
	}
	m = append(m, eq...)
	if f.NoPod {
		m = append(m, lblPod+`=""`)
	}
	if ns := s.nsLimit(); len(ns) > 0 {
		m = append(m, lblNamespace+"=~"+quote(regexAny(ns)))
	}
	if cl := s.clLimit(); len(cl) > 0 {
		m = append(m, lblCluster+"=~"+quote(regexAny(cl)))
	}
	if len(s.FocusServices) > 0 {
		m = append(m, lblService+"=~"+quote(regexAny(s.FocusServices)))
	}
	return m, nil
}

// selectors is what a read sends: one selector, or, when the caller chose services (an application) and did not name one
// itself, that selector plus one per workload kind. Pod metrics (k8s_pod_cpu_usage, restarts, ...) carry the name of
// their Deployment, StatefulSet or DaemonSet and no service name, so without the extra selectors choosing an application
// would leave them out. The selectors are disjoint in what they add, so the results are simply united.
func (f MetricFilter) selectors(s Scope) ([]string, error) {
	if len(s.Focus) > 0 {
		return f.placeSelectors(s)
	}
	base, err := f.selector(s)
	if err != nil {
		return nil, err
	}
	if len(s.FocusServices) == 0 || f.Service != "" {
		return []string{base}, nil
	}
	wide := s
	wide.FocusServices = nil
	m, err := f.matchers(wide)
	if err != nil {
		return nil, err
	}
	out := []string{base}
	for _, l := range lblWorkloads {
		out = append(out, "{"+strings.Join(append(slices.Clone(m), l+"=~"+quote(regexAny(s.FocusServices))), ",")+"}")
	}
	return out, nil
}

// placeSelectors are the selectors of an exact focus: for each place of the application, its names in its own namespace and
// cluster, as the service name and (unless the caller named a service) as each workload label. A series matches one of them
// only if its service or workload is a member where that member runs.
func (f MetricFilter) placeSelectors(s Scope) ([]string, error) {
	wide := s
	wide.FocusServices = nil // the places name the services, each where it runs
	m, err := f.matchers(wide)
	if err != nil {
		return nil, err
	}
	nameLabels := []string{lblService}
	if f.Service == "" {
		nameLabels = append(nameLabels, lblWorkloads...)
	}
	var out []string
	for _, l := range nameLabels {
		for _, c := range placeConds(s.Focus, lblNamespace, lblCluster, l, func(k, v string) string { return k + "=" + quote(v) },
			func(k string, vs []string) string { return k + "=~" + quote(regexAny(vs)) }) {
			out = append(out, "{"+strings.Join(append(slices.Clone(m), c...), ",")+"}")
		}
	}
	return out, nil
}

func (f MetricFilter) selector(s Scope) (string, error) {
	m, err := f.matchers(s)
	if err != nil {
		return "", err
	}
	return "{" + strings.Join(m, ",") + "}", nil
}
