package fusionapi

import (
	"strings"
	"testing"
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
