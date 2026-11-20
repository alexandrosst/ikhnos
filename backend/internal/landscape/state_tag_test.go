package landscape

import (
	"testing"

	"ikhnos/internal/model"
)

// The tag of the state is what lets a poll be answered 304, so it must move with anything the UI draws from. The one that matters most for the
// canvas: a link that has gone quiet (no traffic for longer than the quiet window) is a change, and the time the answer was made is not.
func TestStateTagFollowsWhatTheDocumentSaysNotWhenItWasMade(t *testing.T) {
	live := StateDoc{GeneratedAt: "2026-01-01T00:00:00Z", Topology: model.Topology{Dependencies: []model.Dependency{{ID: "d1"}}}}
	later := live
	later.GeneratedAt = "2026-01-01T00:05:00Z"
	quiet := later
	quiet.Topology.Dependencies = []model.Dependency{{ID: "d1", Stale: true}}

	a, ok1 := stateTag(live)
	b, ok2 := stateTag(later)
	c, ok3 := stateTag(quiet)
	if !ok1 || !ok2 || !ok3 {
		t.Fatal("a document that encodes must have a tag")
	}
	if a != b {
		t.Fatalf("only the time differs, the tag must not: %s vs %s", a, b)
	}
	if a == c {
		t.Fatalf("a link going quiet must move the tag, or a poller would keep drawing it as live: %s", a)
	}
}
