package agent

import (
	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// linkCapabilities is what this install can do for the cluster-link telemetry, for the heartbeat. It is cheap and carries no cluster data.
// Cluster links being off is a report of its own, with every switch false, so the server can tell "off here" (this agent knows the field)
// from "an agent too old to say" (no message at all).
func (r *runner) linkCapabilities() *ikhnosv1.LinkCapabilities {
	h := r.cfg.LinkHub
	if h == nil || h.Forwarder == nil {
		return &ikhnosv1.LinkCapabilities{}
	}
	c := &ikhnosv1.LinkCapabilities{LinkMetrics: true, ProbeEnabled: h.Install.Probe, CapacityEnabled: h.Install.Capacity, TelemetryExport: h.Install.TelemetryExport}
	if t := h.Forwarder.ProberSeen(); !t.IsZero() {
		c.ProberSeenUnix = t.Unix()
	}
	return c
}
