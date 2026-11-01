package fusionapi

import (
	"context"
	"testing"
)

// The gateway's counters say something about every sender, so only a caller with no limit may ask, and only one that may read metrics.
func TestOnlyAnUnrestrictedCallerMayAskTheGatewaysTraffic(t *testing.T) {
	c := &Client{Prometheus: "http://127.0.0.1:1"} // never reached
	for name, s := range map[string]Scope{
		"limited to a namespace": {Signals: Signals, Namespaces: []string{"shop"}},
		"limited to a cluster":   {Signals: Signals, Clusters: []string{"c1"}},
		"not allowed metrics":    {Signals: []string{SignalLogs}},
	} {
		if tr, err := c.GatewayTraffic(context.Background(), s); err == nil || tr != nil {
			t.Errorf("%s: %+v, %v", name, tr, err)
		}
	}
}
