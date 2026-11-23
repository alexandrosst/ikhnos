package fusion

import (
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/operators"
)

// Component names one of the workloads the switch drives.
type Component string

// The workloads of FUSION.
const (
	ComponentMetrics Component = "metrics" // Prometheus
	ComponentLogs    Component = "logs"    // Loki
	ComponentTraces  Component = "traces"  // Tempo
	ComponentGrafana Component = "grafana"
	ComponentCentral Component = "central" // the central operator, the one door in
)

// WireWords is the list of Component's words, as the generated types give it.
func (Component) WireWords() []string {
	return []string{string(ComponentMetrics), string(ComponentLogs), string(ComponentTraces), string(ComponentGrafana), string(ComponentCentral)}
}

// FusionState is where the switch stands.
type FusionState string

// The states of the switch.
const (
	StateOff       FusionState = "off"
	StateStarting  FusionState = "starting"
	StateRunning   FusionState = "running"
	StateAttention FusionState = "attention"
)

// WireWords is the list of FusionState's words, as the generated types give it.
func (FusionState) WireWords() []string {
	return []string{string(StateOff), string(StateStarting), string(StateRunning), string(StateAttention)}
}

// FusionReason is why the switch cannot be used from here (the state is then off).
type FusionReason string

// The reasons the switch is not available.
const (
	ReasonNotConfigured FusionReason = "not-configured" // the server was installed without it, or without the permission to scale it
	ReasonNotInstalled  FusionReason = "not-installed"  // FUSION's workloads are not in this release
	ReasonNoAccess      FusionReason = "no-access"      // the server may not read or scale them
	ReasonOtherOrg      FusionReason = "other-org"      // FUSION is managed from the server's main organisation
)

// WireWords is the list of FusionReason's words, as the generated types give it.
func (FusionReason) WireWords() []string {
	return []string{string(ReasonNotConfigured), string(ReasonNotInstalled), string(ReasonNoAccess), string(ReasonOtherOrg)}
}

// FusionComponent is one workload's state, as the UI shows it.
type FusionComponent struct {
	Component Component `json:"component"`
	Label     string    `json:"label"`
	Desired   int       `json:"desired"`
	Ready     int       `json:"ready"`
	// Reason is, in plain words, why this part is not ready ("Cannot download the image", "Waiting for a volume"); empty
	// while it is ready, and when the cause cannot be told (no permission to read the pod, the pod does not exist yet).
	Reason string `json:"reason,omitempty"`

	hard     bool // Reason names a fault that will not clear by itself, so waiting longer will not help
	optional bool // not part of whether FUSION is up (Grafana)
}

// componentUp says whether the named workload exists in this install and is fully ready.
func (st FusionStatus) ComponentUp(component Component) bool {
	for _, c := range st.Components {
		if c.Component == component {
			return c.Desired > 0 && c.Ready >= c.Desired
		}
	}
	return false
}

// FusionStatus is the switch's whole state.
type FusionStatus struct {
	Available  bool              `json:"available"`
	Reason     FusionReason      `json:"reason,omitempty"`
	State      FusionState       `json:"state"`
	Message    string            `json:"message,omitempty"`
	Since      *time.Time        `json:"since,omitempty"`
	Components []FusionComponent `json:"components,omitempty"`
	// LastDataAt is when Prometheus last held a fresh sample, so "running" can be told from "running, nothing received
	// yet". Omitted when unknown.
	LastDataAt *time.Time `json:"lastDataAt,omitempty"`
	// LastData is when each store last held something (the newest sample, log line or trace), so a store that has stopped receiving
	// can be told from one that is fine while the others receive. A store that is not up, or holds nothing in the last hours, is
	// left out; the whole field is omitted when none is known.
	LastData *StoreTimes `json:"lastData,omitempty"`
	// Traffic is what the central gateway did with the data in the last few minutes, per kind and per second, as its own counters say:
	// what it took in, what it turned away, what it delivered to each store and what it could not. It tells a store that receives from
	// one that is quiet, and how much. Omitted when the metrics store is not up or holds none of the gateway's counters (then nothing
	// is known, which is not the same as nothing moving).
	Traffic *fusionapi.GatewayTraffic `json:"traffic,omitempty"`
}

// copyTraffic is a copy that shares nothing with what it was made from (the queue figures are pointers).
func copyTraffic(t *fusionapi.GatewayTraffic) *fusionapi.GatewayTraffic {
	if t == nil {
		return nil
	}
	c := *t
	for _, q := range []**float64{&c.Metrics.Queue, &c.Logs.Queue, &c.Traces.Queue} {
		if *q != nil {
			v := **q
			*q = &v
		}
	}
	return &c
}

// StoreTimes is a time for each of the three stores.
type StoreTimes struct {
	Metrics *time.Time `json:"metrics,omitempty"`
	Logs    *time.Time `json:"logs,omitempty"`
	Traces  *time.Time `json:"traces,omitempty"`
}

// Managed is what the operators module needs of this state to report the central operator's health.
func (st FusionStatus) Managed() operators.ManagedStatus {
	return operators.ManagedStatus{Available: st.Available, State: string(st.State), LastDataAt: st.LastDataAt}
}

// clone is a copy that shares nothing with the cached one, so a caller may change what it was given.
func (st FusionStatus) clone() FusionStatus {
	st.Components = append([]FusionComponent(nil), st.Components...)
	if st.Since != nil {
		t := *st.Since
		st.Since = &t
	}
	if st.LastDataAt != nil {
		t := *st.LastDataAt
		st.LastDataAt = &t
	}
	if st.LastData != nil {
		d := *st.LastData
		for _, p := range []**time.Time{&d.Metrics, &d.Logs, &d.Traces} {
			if *p != nil {
				t := **p
				*p = &t
			}
		}
		st.LastData = &d
	}
	st.Traffic = copyTraffic(st.Traffic)
	return st
}
