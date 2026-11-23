package fusionapi

import (
	"context"
	"time"
)

// What ikhnos itself knows about the things a trace ran on, joined to the fused object: which services each service
// calls and is called by (the observed topology), and what changed around the trace (scalings, restarts, node and
// cluster events). The data lives in the server, not in the three stores, so the server hands it over through Extras.

// Keys of Fused.Sources for these parts.
const (
	SourceTopology = "topology"
	SourceChanges  = "changes"
)

// Extras is what the server provides beyond the stores. A nil Extras means this server has none, and a fused read that
// asks for them says "unavailable".
type Extras interface {
	// Topology is the landscape as the server last worked it out.
	Topology(ctx context.Context) (*TopologyView, error)
	// Applications are the ikhnos applications of the organisation (groups of services a person or discovery made) and the
	// services in each, as of the last saved workspace.
	Applications(ctx context.Context) ([]AppGroup, error)
	// Changes lists events recorded between since and until, newest last, at most limit of them. clusters narrows them to
	// those ikhnos cluster ids, and targetIDs to the events about those ids (services, dependencies, applications, nodes,
	// clusters), when they are not empty.
	Changes(ctx context.Context, since, until time.Time, clusters, targetIDs []string, limit int) ([]ChangeEvent, error)
}
