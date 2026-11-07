package landscape

import (
	"testing"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

func syncMsg(idx, total uint32) *ikhnosv1.AgentMessage {
	return &ikhnosv1.AgentMessage{Msg: &ikhnosv1.AgentMessage_Sync{Sync: &ikhnosv1.Sync{ChunkIndex: idx, ChunkTotal: total}}}
}

// The pieces after the first of a picture used to cost nothing, so a stream could send the first piece, then thousands
// of the rest, again and again, each making the server read the agent's record.
func TestThePiecesOfAPictureHaveABudgetToo(t *testing.T) {
	l := newStreamLimits()
	// One picture split as far as it may be gets through whole.
	for i := uint32(0); i < maxChunkTotal; i++ {
		if !l.allow(syncMsg(i, maxChunkTotal)) {
			t.Fatalf("piece %d of the largest picture was refused", i)
		}
	}
	// A second one straight after is not: the pieces' budget is spent, whatever the first-piece budget says.
	refused := false
	for i := uint32(0); i < maxChunkTotal && !refused; i++ {
		refused = !l.allow(syncMsg(1+i%(maxChunkTotal-1), maxChunkTotal))
	}
	if !refused {
		t.Fatal("a stream sent pieces without end")
	}
}

func TestTheFirstPieceAndPlainMessagesAreStillCounted(t *testing.T) {
	l := newStreamLimits()
	plain := &ikhnosv1.AgentMessage{Msg: &ikhnosv1.AgentMessage_Heartbeat{Heartbeat: &ikhnosv1.Heartbeat{}}}
	ok := 0
	for i := 0; i < 1000 && l.allow(plain); i++ {
		ok++
	}
	if ok != 20 {
		t.Fatalf("burst of plain messages: %d, want 20", ok)
	}
	l = newStreamLimits()
	ok = 0
	for i := 0; i < 1000 && l.allow(syncMsg(0, 5)); i++ {
		ok++
	}
	if ok != 20 {
		t.Fatalf("burst of first pieces: %d, want 20", ok)
	}
}

func TestASeriesOfPiecesDoesNotUseTheMessageBudget(t *testing.T) {
	l := newStreamLimits()
	for i := uint32(1); i <= 200; i++ {
		if !l.allow(syncMsg(i, 300)) {
			t.Fatalf("piece %d refused", i)
		}
	}
	if !l.allow(syncMsg(0, 300)) {
		t.Fatal("pieces used up the budget of ordinary messages")
	}
}
