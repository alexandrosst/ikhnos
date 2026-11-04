package graph

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The read of the past is two statements in one, and a write between them can show a version in both; whatever order Neo4j answers in,
// the entities come out once each, in order.
func TestEntitiesOfAMomentComeOutOnceEachAndInOrder(t *testing.T) {
	in := []EntitySnapshot{
		{Kind: "service", ID: "b"}, {Kind: "cluster", ID: "z"}, {Kind: "service", ID: "a"}, {Kind: "service", ID: "b"}, {Kind: "cluster", ID: "a"},
	}
	got := tidyEntities(in)
	var keys []string
	for _, e := range got {
		keys = append(keys, e.Kind+"/"+e.ID)
	}
	if want := []string{"cluster/a", "cluster/z", "service/a", "service/b"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("%v", keys)
	}
	if len(tidyEntities(nil)) != 0 {
		t.Fatal("nothing in, nothing out")
	}
}

// An entity with no stored document is a null, not an empty string, which is no JSON and fails the whole answer.
func TestAnEntityWithNoDocumentIsNullAndTheAnswerStillMarshals(t *testing.T) {
	b, err := json.Marshal([]EntitySnapshot{{Kind: "agent", ID: "a1", Doc: rawDoc("")}, {Kind: "service", ID: "s", Doc: rawDoc(`{"x":1}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[{"kind":"agent","id":"a1","doc":null},{"kind":"service","id":"s","doc":{"x":1}}]` {
		t.Fatalf("%s", b)
	}
}
