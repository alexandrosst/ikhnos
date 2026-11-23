package fusionapi

import (
	"strings"
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
)

// Every selector a limited caller's query reads through carries its limits: inside a range, a subquery, both sides of a
// binary operation, under label_replace; and a query that would undo the limits by the way it is written cannot, because the
// limits are on what is read, not on what comes out.
func TestALimitedCallersPromQLReadsOnlyWhatItMaySee(t *testing.T) {
	s := Scope{Signals: Signals, Namespaces: []string{"shop"}}
	for _, expr := range []string{
		`up`,
		`sum(rate(http_requests_total[5m]))`,
		`max_over_time(rate(x[1m])[30m:1m])`,
		`a / on() group_left b`,
		`label_replace(up{k8s_namespace_name="secret"}, "k8s_namespace_name", "shop", "", "")`,
		`{__name__=~".+"}`,
		`count({k8s_namespace_name!="shop",job="x"})`,
	} {
		got, err := restrictPromQL(expr, s)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		// as many limits as selectors
		sels := strings.Count(got, "{")
		if n := strings.Count(got, `k8s_namespace_name=~"shop"`); n != sels || n == 0 {
			t.Errorf("%s -> %s: %d selectors, %d limited", expr, got, sels, n)
		}
	}
	if got, _ := restrictPromQL(`up{k8s_namespace_name="secret"}`, s); !strings.Contains(got, `k8s_namespace_name="secret"`) || !strings.Contains(got, `k8s_namespace_name=~"shop"`) {
		t.Fatalf("asking for another namespace keeps both matchers, and so matches nothing: %s", got)
	}
	if got, _ := restrictPromQL(`sum(up)`, AllSignals()); got != `sum(up)` {
		t.Fatalf("an unlimited caller's query is sent as written: %s", got)
	}
	if _, err := restrictPromQL(`info(up)`, s); err == nil {
		t.Fatal("info() reads series of its own, past the selectors")
	}
	if _, err := restrictPromQL(`sum(`, s); err == nil {
		t.Fatal("a query that does not parse is refused here, not sent")
	}
	// A value with a regular expression's characters is a literal in the limit.
	got, _ := restrictPromQL(`up`, Scope{Signals: Signals, Clusters: []string{"cl.1"}})
	if !strings.Contains(got, `ikhnos_cluster_id=~"cl\\.1"`) {
		t.Fatalf("%s", got)
	}
}

// A narrowed query may call Prometheus's stable functions only: info() reads series of its own, and an experimental
// function may change what it reads, so neither is taken whether or not experimental functions are enabled. The refusal
// is right for the caller who gets it: a limited token cannot use the function at all, and a caller who only narrowed
// the read can send it as written.
func TestANarrowedQueryMayCallOnlyStableFunctions(t *testing.T) {
	limitedScope := Scope{Signals: Signals, Namespaces: []string{"shop"}}
	app := &AppGroup{ID: "app", Name: "Shop", Members: []AppMember{{Name: "api", Namespace: "shop", Cluster: "c1"}}}
	for _, enabled := range []bool{false, true} {
		was := parser.EnableExperimentalFunctions
		parser.EnableExperimentalFunctions = enabled
		for _, expr := range []string{`info(up)`, `sort_by_label(up, "job")`, `sum(mad_over_time(up[5m]))`} {
			_, err := restrictPromQL(expr, limitedScope)
			if err == nil || statusOf(err) != 400 || !strings.Contains(err.Error(), "limited to certain namespaces or clusters") ||
				strings.Contains(err.Error(), "promql_as_written") {
				t.Errorf("experimental enabled %v, %s from a limited token: %v", enabled, expr, err)
			}
			_, err = narrowQuery(expr, FuseOptions{Application: app})
			if err == nil || !strings.Contains(err.Error(), "narrowed to an application") ||
				!strings.Contains(err.Error(), "promql_as_written=true") ||
				strings.Contains(err.Error(), "only for a caller whose access is not limited") {
				t.Errorf("experimental enabled %v, %s narrowed to an application: %v", enabled, expr, err)
			}
		}
		parser.EnableExperimentalFunctions = was
	}
	_, err := restrictPromQL(`sort_by_label(up, "job")`, limitedScope)
	if err == nil || !strings.Contains(err.Error(), "sort_by_label() is an experimental") {
		t.Fatalf("the refusal does not name the function: %v", err)
	}
	for _, expr := range []string{
		`rate(x[5m])`, `label_replace(up, "a", "b", "", "")`, `histogram_quantile(0.9, sum by (le) (rate(h[5m])))`,
	} {
		if _, err := restrictPromQL(expr, limitedScope); err != nil {
			t.Errorf("a stable function was refused: %s: %v", expr, err)
		}
	}
	if got, err := restrictPromQL(`info(up)`, AllSignals()); err != nil || got != `info(up)` {
		t.Fatalf("an unlimited caller's query is sent as written, for Prometheus to judge: %q %v", got, err)
	}
}
