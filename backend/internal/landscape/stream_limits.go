package landscape

import (
	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/kernel"
)

// streamLimits is how fast one agent's stream may talk. Every message costs the server a look at the agent's
// record (revocation applies mid-stream), so none is free; but the pieces of one large picture are many by design
// and come together, so they have a budget of their own.
type streamLimits struct {
	messages *kernel.Limiter // messages, and the first piece of a picture
	pieces   *kernel.Limiter // the later pieces of a picture that comes in several
}

func newStreamLimits() *streamLimits {
	return &streamLimits{
		messages: kernel.NewLimiter(120, 20), // per minute, burst
		// One picture at its most is maxChunkTotal pieces and must get through in one go; after that the budget
		// comes back at a rate a sound agent never needs, so a stream cannot keep the server busy with pieces.
		pieces: kernel.NewLimiter(2048, maxChunkTotal),
	}
}

// allow takes the cost of a message and says whether it may be handled.
func (l *streamLimits) allow(m *ikhnosv1.AgentMessage) bool {
	if sy := m.GetSync(); sy != nil && isChunk(sy) && sy.ChunkIndex > 0 {
		return l.pieces.Allow("p")
	}
	return l.messages.Allow("s")
}
