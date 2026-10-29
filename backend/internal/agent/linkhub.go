package agent

import (
	"context"
	"fmt"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/link"
	"ikhnos/internal/linkhub"

	"google.golang.org/protobuf/proto"
)

// LinkHub is the agent's side of the cluster-link hub: the forwarder that sends it the rows and the configuration, and what the chart says the
// install around it can do (which the hub cannot know, and which the agent reports to the server with its capabilities).
type LinkHub struct {
	Forwarder *linkhub.Forwarder
	Install   link.Install
	// Iperf says the install hands iperf3 credentials to the link pods. Without it the credentials the server issues are not passed on, so the
	// hub never holds a key that no pod of the install will ask for.
	Iperf bool
}

// linkHubUnreachableAfter is how long the hub may go without answering before the agent says so.
const linkHubUnreachableAfter = 2 * time.Minute

// linkHubLoop runs the forwarder until ctx ends, starting it again after a panic: it is the only thing that carries the rows and the
// configuration to the hub, so it ending quietly would leave the series stale with nothing to say why.
func (r *runner) linkHubLoop(ctx context.Context) {
	for ctx.Err() == nil {
		r.safely("link hub forwarder", func() { r.cfg.LinkHub.Forwarder.Run(ctx) })
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// linkHubSlice is what the hub is told of a Config: the peers the server named and the credentials, unless the install passes none on. The same
// rules as when the agent served them itself: nothing below access tier 2 or while the flow collector is paused.
func (r *runner) linkHubSlice(c *ikhnosv1.Config, peers []*ikhnosv1.LinkPeer, iperf *ikhnosv1.LinkIperfServer) *ikhnosv1.Config {
	h := r.cfg.LinkHub
	if !h.Iperf {
		iperf = nil
		stripped := make([]*ikhnosv1.LinkPeer, len(peers))
		for i, p := range peers {
			cp := proto.Clone(p).(*ikhnosv1.LinkPeer)
			cp.IperfClient = nil
			stripped[i] = cp
		}
		peers = stripped
	}
	return &ikhnosv1.Config{ClusterId: c.ClusterId, LinkPeers: peers, LinkIperfServer: iperf}
}

// linkHubProblem says the hub cannot be reached, once it has been unreachable for linkHubUnreachableAfter: the link series are stale and the
// prober and the responders are working from what the hub last held. Empty when all is well.
func (r *runner) linkHubProblem(now time.Time) string {
	h := r.cfg.LinkHub
	if h == nil || h.Forwarder == nil {
		return ""
	}
	st := h.Forwarder.Stats()
	ref := st.LastOK
	if st.Started.After(ref) {
		ref = st.Started
	}
	if st.Reached || now.Sub(ref) < linkHubUnreachableAfter {
		return ""
	}
	why := st.LastError
	if why == "" {
		why = "it has not answered"
	}
	return fmt.Sprintf("The cluster-link hub has not answered for more than %s (%s). The link series stop growing and the prober and the responders keep what the hub last held; %d rows are waiting to be sent, %d were dropped. Check that its pod is running (kubectl -n %s get pods) and that the network policy lets this agent reach it.", linkHubUnreachableAfter, clipMsg(why, 160), st.Queued, st.Dropped, "ikhnos-system")
}
