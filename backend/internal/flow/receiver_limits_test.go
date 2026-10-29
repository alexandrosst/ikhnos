package flow

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/flow/wire"
	"ikhnos/internal/probe"

	"google.golang.org/protobuf/encoding/protojson"
)

// The node probe and the flow collectors are separate pods with separate secrets. Their signatures are also bound to different contexts, so that
// a signature made for one is not valid for the other even if the secrets were ever given the same value.
func TestAProbeSignatureIsNotAFlowSignatureEvenWithTheSameSecret(t *testing.T) {
	secret := []byte("one-secret-0123456789abcdef0123")
	p := NewPipeline(secret, testIndex, nil)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	body, _ := protojson.Marshal(&ikhnosv1.FlowReport{Method: "ebpf", Node: "n1"})
	post := func(sign func(ts string) string) int {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req, _ := http.NewRequest("POST", srv.URL+PathReport, bytes.NewReader(body))
		req.Header.Set(probe.HeaderNode, "n1")
		req.Header.Set(probe.HeaderTime, ts)
		req.Header.Set(probe.HeaderSig, sign(ts))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post(func(ts string) string { return probe.Sign(secret, ts, "n1", body) }); c != http.StatusUnauthorized {
		t.Errorf("signed the way the node probe signs: %d, want 401", c)
	}
	if c := post(func(ts string) string { return probe.SignContext(wire.SignContext, secret, ts, "n1", body) }); c != http.StatusNoContent {
		t.Errorf("signed the way a flow collector signs: %d, want 204", c)
	}
}

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) {
	b.t.Error("a request from a source over its rate was read")
	return 0, errors.New("must not be read")
}
func (unreadBody) Close() error { return nil }

// One address that sends far faster than a collector does is turned away before its body is read, and the collectors of the other nodes go on.
func TestOneSourceCannotKeepTheFlowReceiverBusy(t *testing.T) {
	secret := []byte("flow-secret-0123456789abcdef0123")
	p := NewPipeline(secret, testIndex, nil)
	now := time.Now()
	p.now = func() time.Time { return now }
	p.SetSourceLimit(probe.NewSourceLimit(time.Second, 2, 100))
	h := p.Handler()
	send := func(from, node string) (int, string) {
		body, _ := protojson.Marshal(&ikhnosv1.FlowReport{Method: "ebpf", Node: node})
		ts := strconv.FormatInt(now.Unix(), 10)
		req := httptest.NewRequest("POST", PathReport, bytes.NewReader(body))
		req.RemoteAddr = from
		req.Header.Set(probe.HeaderNode, node)
		req.Header.Set(probe.HeaderTime, ts)
		req.Header.Set(probe.HeaderSig, probe.SignContext(wire.SignContext, secret, ts, node, body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code, w.Header().Get("Retry-After")
	}
	for i, node := range []string{"n1", "n2"} {
		if c, _ := send("10.0.0.7:5000", node); c != http.StatusNoContent {
			t.Fatalf("request %d of the burst: %d", i+1, c)
		}
	}
	if c, after := send("10.0.0.7:5001", "n3"); c != http.StatusTooManyRequests || after != "2" {
		t.Fatalf("over the rate: %d, Retry-After %q", c, after)
	}
	req := httptest.NewRequest("POST", PathReport, unreadBody{t})
	req.RemoteAddr = "10.0.0.7:5002"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("unread body: %d", w.Code)
	}
	if c, _ := send("10.0.0.8:5000", "n4"); c != http.StatusNoContent {
		t.Fatalf("another node's collector was held up by the first: %d", c)
	}
	now = now.Add(time.Second)
	if c, _ := send("10.0.0.7:5003", "n5"); c != http.StatusNoContent {
		t.Fatalf("after a token was back: %d", c)
	}
}

// What the collector needs to know to post a report is in package wire, and the collector links that and not this package, whose resolver
// brings in the Kubernetes client.
func TestTheWireConstantsAreTheOnesTheReceiverUses(t *testing.T) {
	if PathReport != wire.PathReport || MaxBody != wire.MaxBody || MaxRawFlows != wire.MaxRawFlows {
		t.Fatal("package flow names constants other than the ones package wire holds")
	}
}
