package wire

// What the node flow collectors and the agent's receiver both have to know about a report, kept here and not in package flow so that the
// collector, which runs on every node, does not link the agent's side (the resolver and the Kubernetes client behind it) to learn a path.
const (
	// PathReport is where a collector posts its report on the agent's receiver.
	PathReport = "/v1/flows"
	// MaxBody bounds one report; a collector sends every 30 seconds. Nothing beyond it is read.
	MaxBody = 512 << 10
	// MaxRawFlows bounds the observations in one report, and is chosen so that a report of this many
	// flows, with every field at its longest, still fits in MaxBody (a test holds that). The collector
	// keeps the busiest flows and counts the rest as lost.
	MaxRawFlows = 1500
	// SignContext is what a flow report's signature is bound to (see probe.SignContext). The node probe's reports are bound to another,
	// so a signature made for one is not valid for the other even if the two secrets were ever the same.
	SignContext = "ikhnos-flow-v1"
)
