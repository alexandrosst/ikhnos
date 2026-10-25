// Package relay is what the agent and the controller say to each other through the status of an IkhnosAgent: the agent writes
// status.requested, the telemetry an administrator asked the cluster to collect (the server sent it to the agent in its Config), and the
// controller writes status.relay, what became of it. Neither reads the other's code: this package is the whole of what passes, and it
// holds nothing else, so that both are written against one definition and a test can hold the resource's schema to it.
//
// It is the "what" of the telemetry and never the "where": there is no destination here, and no credential.
package relay

// The two keys of an IkhnosAgent's status that belong to the relay. The agent owns the first and the controller the second; each leaves
// the other's alone.
const (
	StatusRequested = "requested"
	StatusRelay     = "relay"
	// StatusWorkloads is the controller's too, and is not part of the relay: what the release runs and how many pods of each are ready. The agent
	// only reads it, to tell the server.
	StatusWorkloads = "workloads"
)

// The states of a request, as the controller says them (Result.State). "written" is said by the agent, before the controller has said
// anything: the request is where the controller reads it.
const (
	Written  = "written"
	Applied  = "applied"
	Refused  = "refused"
	Reverted = "reverted"
)

// Signals are the telemetry signals of the resource (spec.telemetry.signals), named as the resource names them.
type Signals struct {
	ResourceUsage      bool `json:"resourceUsage,omitempty"`
	Energy             bool `json:"energy,omitempty"`
	KubernetesState    bool `json:"kubernetesState,omitempty"`
	NodeRuntime        bool `json:"nodeRuntime,omitempty"`
	NetworkLatency     bool `json:"networkLatency,omitempty"`
	ClusterLinks       bool `json:"clusterLinks,omitempty"`
	ApplicationMetrics bool `json:"applicationMetrics,omitempty"`
	SystemLogs         bool `json:"systemLogs,omitempty"`
	KubernetesEvents   bool `json:"kubernetesEvents,omitempty"`
	ApplicationLogs    bool `json:"applicationLogs,omitempty"`
	Traces             bool `json:"traces,omitempty"`
	Accelerators       bool `json:"accelerators,omitempty"`
}

// Request is what the agent writes under status.requested. The Revision is the server's: it rises with every change, including taking the
// request back, which is a revision with no signals and no share of the traces (the cluster's own settings then stand).
type Request struct {
	Revision uint64 `json:"revision"`
	// Signals is the whole set to collect; a signal not named is off. Nil: no request about signals.
	Signals *Signals `json:"signals,omitempty"`
	// TracesSamplingPercentage is how much of the traces to keep, 1 to 100. Nil: no request, the cluster's own value stands.
	TracesSamplingPercentage *int `json:"tracesSamplingPercentage,omitempty"`
}

// Asks is whether the request asks for anything. One that does not is the request taken back.
func (r Request) Asks() bool { return r.Signals != nil || r.TracesSamplingPercentage != nil }

// Result is what the controller writes under status.relay: the revision it is about, where that revision got to, and why when it did not
// get far.
type Result struct {
	Revision uint64 `json:"revision"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
}

// Workload is one thing the release runs (the agent, a collector, Kepler, the node probe) and how many of its pods are ready: what the
// controller writes under status.workloads, and the agent reads.
type Workload struct {
	Name string `json:"name"`
	// Kind is Deployment or DaemonSet.
	Kind string `json:"kind"`
	// Component is the part of the release it is, from its app.kubernetes.io/component label (the agent's own Deployment has none).
	Component string `json:"component,omitempty"`
	// Wanted is how many pods it asks for: its replicas, or the nodes a DaemonSet is to run on.
	Wanted int32 `json:"wanted"`
	// Ready is how many of them are running, updated and available.
	Ready int32 `json:"ready"`
	// RollingOut says the last change to it has not been carried out yet.
	RollingOut bool `json:"rollingOut,omitempty"`
}
