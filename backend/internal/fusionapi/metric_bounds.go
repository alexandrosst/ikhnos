package fusionapi

import (
	"regexp"
	"time"
)

// Step limits.
const (
	minStep   = 15 * time.Second
	maxPoints = 5000
)

// ChooseStep picks the resolution of a range query: the requested step if it gives a sane number of points,
// otherwise about target points across the range, never finer than minStep.
func ChooseStep(tr TimeRange, requested time.Duration, target int) (time.Duration, error) {
	span := tr.To.Sub(tr.From)
	if requested > 0 {
		if requested < time.Second {
			return 0, badRequest("step must be at least one second")
		}
		if int(span/requested) > maxPoints {
			return 0, badRequest("step gives more than %d points over this range; use a larger step or a shorter range", maxPoints)
		}
		return requested, nil
	}
	if target <= 0 {
		target = 120
	}
	step := span / time.Duration(target)
	if step < minStep {
		step = minStep
	}
	return step.Round(time.Second), nil
}

// NeedSelective refuses a filter that would read every series there is: Prometheus evaluates the selector before it applies
// any limit, so a range read with only a category or a cluster to go by costs the whole TSDB. A metric name, a name pattern,
// a service, namespace, pod or node, or an application (the Scope's focus) is enough.
func (f MetricFilter) NeedSelective(s Scope) error {
	if f.Name != "" || (f.NameRegex != "" && !matchesAnyName(f.NameRegex)) || f.Service != "" || f.Namespace != "" || f.Pod != "" || f.Node != "" || len(s.FocusServices) > 0 {
		return nil
	}
	return badRequest("a range read needs a filter: name, metric (a pattern that picks some names, not all), service, namespace, pod, node or application")
}

// matchesAnyName says whether a metric name pattern lets every name through (.+, .*, .) or the empty one: it then selects what no
// pattern does, and costs what no filter does. Prometheus anchors the pattern, as RE2 here is told to.
func matchesAnyName(pattern string) bool {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		return false // refused where it is used
	}
	if re.MatchString("") {
		return true
	}
	for _, probe := range []string{"a", "z_9", "k8s_pod_cpu_time", "http_server_request_duration_seconds_bucket", "up", "0"} {
		if !re.MatchString(probe) {
			return false
		}
	}
	return true
}
