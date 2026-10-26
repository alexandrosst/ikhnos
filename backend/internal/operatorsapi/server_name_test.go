package operatorsapi

import (
	"testing"

	"ikhnos/internal/operators"
	"ikhnos/internal/store"
)

// A certificate is checked against the name the sender dials, so what the sender dials decides whether the operator's stable name has to be
// said, not only what the server has recorded of the operator: a central operator reached at an IP address (its address not known to the server
// at that moment) carried no name, and the collectors could not verify it ("doesn't contain any IP SANs").
func TestTheStableNameIsStatedForAnyAddressThatIsNotAServiceOfTheCluster(t *testing.T) {
	central := store.Operator{ID: operators.CentralOperatorID}
	for _, c := range []struct {
		endpoint string
		want     bool
	}{
		{"147.102.13.108:4317", true},
		{"fusion.example.org:4317", true},
		{"[2001:db8::1]:4317", true},
		{"ikhnos-server-fusion-central.ikhnos.svc:4317", false},
		{"ikhnos-server-fusion-central.ikhnos.svc.cluster.local:4317", false},
	} {
		if got := operators.OperatorNeedsServerNameAt(central, c.endpoint); got != c.want {
			t.Errorf("central at %s: %v, want %v", c.endpoint, got, c.want)
		}
		reach := OperatorReach(central, c.endpoint)
		if (reach.ServerName != "") != c.want || (c.want && reach.ServerName != operators.OperatorServerName(central)) {
			t.Errorf("central at %s: server name %q", c.endpoint, reach.ServerName)
		}
	}
	// A regional operator is always verified by its stable name, and one with an address recorded too.
	if !operators.OperatorNeedsServerNameAt(store.Operator{ID: "op-eu"}, "op-eu-regional-operator.ikhnos-system.svc:4317") {
		t.Error("a regional operator is verified by its stable name")
	}
	if !operators.OperatorNeedsServerNameAt(store.Operator{ID: operators.CentralOperatorID, Address: "1.2.3.4:4317"}, "ikhnos-server-fusion-central.ikhnos.svc:4317") {
		t.Error("a central operator with an advertised address is verified by its stable name")
	}
	// The block of values the helm command carries says it too.
	var name string
	for _, v := range ExportBlock("telemetry.export.otlp", central, "147.102.13.108:4317", true) {
		if v.Key == "telemetry.export.otlp.tls.serverName" {
			name = v.Value
		}
	}
	if name != operators.OperatorServerName(central) {
		t.Errorf("the helm block states server name %q", name)
	}
}
