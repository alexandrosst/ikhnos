package relay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTheRequestAndTheResultAreWhatTheStatusHolds(t *testing.T) {
	share := 20
	b, err := json.Marshal(Request{Revision: 7, Signals: &Signals{Traces: true, ResourceUsage: true}, TracesSamplingPercentage: &share})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"revision":7,"signals":{"resourceUsage":true,"traces":true},"tracesSamplingPercentage":20}`; got != want {
		t.Fatalf("%s\nwant %s", got, want)
	}
	// Taking the request back is a revision with nothing else in it.
	b, _ = json.Marshal(Request{Revision: 8})
	if string(b) != `{"revision":8}` {
		t.Fatalf("%s", b)
	}
	b, _ = json.Marshal(Result{Revision: 7, State: Refused, Reason: "no"})
	if string(b) != `{"revision":7,"state":"refused","reason":"no"}` {
		t.Fatalf("%s", b)
	}
}

func TestARequestAsksForSomethingOnlyWhenItNamesSignalsOrAShare(t *testing.T) {
	share := 5
	for _, tc := range []struct {
		r    Request
		asks bool
	}{{Request{Revision: 1}, false}, {Request{Revision: 1, Signals: &Signals{}}, true}, {Request{Revision: 1, TracesSamplingPercentage: &share}, true}} {
		if tc.r.Asks() != tc.asks {
			t.Errorf("%+v asks %v", tc.r, tc.r.Asks())
		}
	}
}

// There is no way to say where data goes in a request: a field that is not here cannot be written by the agent and read by the controller.
func TestARequestHasNoPlaceForADestination(t *testing.T) {
	b, _ := json.Marshal(Request{Revision: 1, Signals: &Signals{}})
	for _, word := range []string{"destination", "endpoint", "secret", "route", "auth", "token"} {
		if strings.Contains(strings.ToLower(string(b)), word) {
			t.Errorf("a request names %q: %s", word, b)
		}
	}
	var r Request
	dec := json.NewDecoder(strings.NewReader(`{"revision":1,"destination":{"endpoint":"evil.example.com:4317"}}`))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) == nil {
		t.Error("a request with a destination in it was read")
	}
}
