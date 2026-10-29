// Package linkhub is the cluster-link measurement hub: the part of the cluster-link telemetry that used to live in the discovery agent.
//
// The agent is the one door between a cluster and the server: it holds the cluster's identity and the stream to the server. The passive
// link series (counts per cluster pair, labelled from the traffic the node collectors observe), the peer list that the link prober asks for and
// the iperf3 credentials that the responder pods ask for were also served from the agent's process. That put two things there that do not
// belong with the identity key: a listener that answers every pod of the cluster (and the Prometheus scrape), and series that restart at zero
// whenever the agent is recreated.
//
// The hub is a pod of its own that holds no credential of the server and no Kubernetes token. It serves the same three things, and it is
// told what to serve by the agent over two messages, both signed with a secret that only the agent and the hub hold:
//
//   - rows: the rows of the flow reports that the agent has already admitted (the resolver's namespace exclusions have been applied), cut
//     down to the fields the series are made of. What a row means, which workload it belonged to and which namespace, is not sent;
//   - config: the slice of the server's Config the hub needs: this cluster's id, the peers to measure and the iperf3 credentials. It is sealed,
//     because it carries a private key.
//
// Each answer carries the hub's status back: which configuration it holds and when the prober last asked. Nothing else crosses.
package linkhub

import (
	"time"
)

const (
	// PathRows and PathConfig are what the agent posts to.
	PathRows   = "/v1/hub/rows"
	PathConfig = "/v1/hub/config"
	// AgentNode is the name the agent signs as; the hub accepts no other.
	AgentNode = "ikhnos-agent"

	rowsContext   = "ikhnos-hub-rows-v1"
	configContext = "ikhnos-hub-config-v1"
	configSeal    = "ikhnos-hub-config-seal-v1"
	digestLabel   = "ikhnos-hub-config-digest-v1"

	// MaxRowsPerPost bounds the rows one request carries and one request may ask the hub to take in.
	MaxRowsPerPost = 4000
	maxRowsBody    = 2 << 20
	maxConfigBody  = 2 << 20
	maxStatusBody  = 64 << 10

	// Version is the version of the two messages. A hub that is sent another answers 400, and says so in its log.
	Version = 1
)

// Status is what the hub answers to both messages: what it holds, so the agent can tell whether it needs to send the configuration again
// (a restarted hub holds none), and when the prober last asked for the peers, which the agent reports to the server with its capabilities.
type Status struct {
	Version int `json:"v"`
	// ConfigDigest identifies the configuration the hub holds (empty: none yet).
	ConfigDigest string `json:"config_digest,omitempty"`
	// ProberSeenUnix is when the prober last made a valid request for the peers (zero: not since the hub started).
	ProberSeenUnix int64 `json:"prober_seen_unix,omitempty"`
	Peers          int   `json:"peers"`
}

// ProberSeen is ProberSeenUnix as a time; the zero time if the prober has not asked.
func (s Status) ProberSeen() time.Time {
	if s.ProberSeenUnix == 0 {
		return time.Time{}
	}
	return time.Unix(s.ProberSeenUnix, 0)
}
