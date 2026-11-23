package fusionapi

import (
	"regexp"
	"slices"
	"strings"
)

// matchers builds the label matchers of a selector: the filter's own, then the Scope's, each as its own matcher so a
// filter that contradicts the Scope simply matches nothing.
func (f MetricFilter) matchers(s Scope) ([]string, error) { return f.matchersIn(s.places()) }

// matchersIn is matchers with the places to read already worked out: the filter's own matchers, then the places' union
// cut (placeSet.cutConds).
func (f MetricFilter) matchersIn(p placeSet) ([]string, error) {
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
	return append(m, p.cutConds(labelSyntax)...), nil
}

// selectors is what a read sends: one selector, or, when the caller chose services (an application) and did not name one
// itself, one with the services' names as the service name plus one per workload kind. Pod metrics (k8s_pod_cpu_usage,
// restarts, ...) carry the name of their Deployment, StatefulSet or DaemonSet and no service name, so without the extra
// selectors choosing an application would leave them out. The selectors are disjoint in what they add, so the results
// are simply united. An exact focus has those selectors once per place of the application, its names in its own namespace
// and cluster, so that a series matches one of them only if its service or workload is a member where that member runs
// (placeSet.promSelectors).
func (f MetricFilter) selectors(s Scope) ([]string, error) {
	p := s.places()
	m, err := f.matchersIn(p.unnamed()) // the names go in each selector, under the label it reads them by
	if err != nil {
		return nil, err
	}
	nameLabels := []string{lblService}
	if f.Service == "" {
		nameLabels = append(nameLabels, lblWorkloads...)
	}
	return p.promSelectors(m, nameLabels), nil
}

func (f MetricFilter) selector(s Scope) (string, error) {
	m, err := f.matchers(s)
	if err != nil {
		return "", err
	}
	return "{" + strings.Join(m, ",") + "}", nil
}
