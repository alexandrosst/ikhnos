package linkhub

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/link"
	"ikhnos/internal/probe"

	"google.golang.org/protobuf/encoding/protojson"
)

var hubSecret = []byte("hub-secret-0123456789abcdef0123")

const (
	testUser     = "ik-test0001"
	testPassword = "pw-0123456789abcdef0123456789abcdef-SECRETMARK"
)

var (
	keyOnce sync.Once
	privPEM string
	pubPEM  string
)

func keys(t testing.TB) (priv, pub string) {
	t.Helper()
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		pd, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
		privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pd}))
	})
	return privPEM, pubPEM
}

func usersCSV() string {
	sum := sha256.Sum256([]byte("{" + testUser + "}" + testPassword))
	return testUser + "," + hex.EncodeToString(sum[:]) + "\n"
}

// serverConfig is a Config as the agent receives it: more than the hub is meant to have.
func serverConfig(t testing.TB) *ikhnosv1.Config {
	priv, pub := keys(t)
	return &ikhnosv1.Config{
		ClusterId:          "cl-here",
		ApprovedAccessTier: 2,
		PausedCollectors:   []string{"flow"},
		LinkPeers: []*ikhnosv1.LinkPeer{
			{ClusterId: "cl-a", Name: "Athens", Addresses: []string{"10.20.0.5", "10.20.0.6"}, Throughput: true, ThroughputBudgetBytes: 1 << 30,
				IperfClient: &ikhnosv1.LinkIperfClient{PublicKeyPem: pub, Username: testUser, Password: testPassword}},
			{ClusterId: "cl-b", Name: "Berlin", Addresses: []string{"10.30.0.9"}},
		},
		LinkIperfServer: &ikhnosv1.LinkIperfServer{PrivateKeyPem: priv, UsersCsv: usersCSV()},
	}
}

func row(peer string, over func(*ikhnosv1.RawFlow)) *ikhnosv1.RawFlow {
	r := &ikhnosv1.RawFlow{Client: true, LocalIp: "10.42.0.7", PeerIp: peer, Port: 443, Protocol: "tcp", Connections: 3, BytesOut: 1000, BytesIn: 2000, SegsOut: 40, Retransmits: 2, RttUs: 12000, Iface: "eth0", SniHost: "payments.internal.example", DnsQueryName: "db.shop.svc"}
	if over != nil {
		over(r)
	}
	return r
}

// rig is a hub behind a real listener and a forwarder pointed at it. The handler is behind a pointer so a test can replace the hub (a restart).
type rig struct {
	t      *testing.T
	hub    *Hub
	meter  *link.Meter
	srv    *httptest.Server
	cur    *Hub
	f      *Forwarder
	mu     sync.Mutex
	bodies [][]byte
	down   bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t}
	r.setHub()
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		down, h := r.down, r.cur
		r.mu.Unlock()
		if down {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, b)
		r.mu.Unlock()
		req.Body = io.NopCloser(bytes.NewReader(b))
		h.Handler().ServeHTTP(w, req)
	}))
	t.Cleanup(r.srv.Close)
	r.f = NewForwarder(r.srv.URL, hubSecret, nil, nil)
	return r
}

func (r *rig) setHub() {
	m := link.NewMeter(link.NewPeers())
	m.ServePeers([]byte("peers-secret-0123456789abcdef"))
	m.ServeIperf([]byte("iperf-secret-0123456789abcdef"))
	h := New(hubSecret, m, nil)
	h.limit = nil
	r.mu.Lock()
	r.hub, r.meter, r.cur = h, m, h
	r.mu.Unlock()
}

func (r *rig) cycle(beat bool) error { return r.f.cycle(context.Background(), beat) }

// scrape is the series without the line that holds a clock.
func scrape(m *link.Meter) string {
	var b bytes.Buffer
	m.Write(&b)
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if !strings.Contains(l, "last_observation_timestamp") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// ---- narrowing ----

// Every field of a row is named here, as kept or as dropped. A field added to the row is in neither list, and this test fails until someone decides
// which it is: the series are made of what is kept, and what is dropped is what leaves the agent only if it is on this list on purpose.
var (
	keptFields    = []string{"Client", "LocalIp", "PeerIp", "Port", "Connections", "BytesOut", "BytesIn", "SegsOut", "Retransmits", "RtoRetransmits", "BufferDrops", "RttUs", "FailedAttempts", "FailedRefused", "FailedTimeout", "FailedReset", "FailedUnreachable"}
	droppedFields = []string{"Protocol", "Iface", "SniHost", "DnsQueryName", "JitterUs", "HandshakeUs", "Cwnd", "PacingBps", "DnsRttUs", "MeshBypassSyns", "MssBytes", "RcvWndBytes", "SndWndBytes", "WmemQueuedBytes", "SndbufBytes", "TlsHandshake", "CgroupId"}
)

func filled(t *testing.T) *ikhnosv1.RawFlow {
	r := &ikhnosv1.RawFlow{}
	v := reflect.ValueOf(r).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.Bool:
			f.SetBool(true)
		case reflect.String:
			f.SetString("10.20.0.5")
			if v.Type().Field(i).Name != "LocalIp" && v.Type().Field(i).Name != "PeerIp" {
				f.SetString("x-" + v.Type().Field(i).Name)
			}
		case reflect.Uint32, reflect.Uint64:
			f.SetUint(uint64(7 + i))
		case reflect.Int32, reflect.Int64:
			f.SetInt(int64(7 + i))
		case reflect.Ptr:
			f.Set(reflect.New(f.Type().Elem())) // a message or an optional number: present is enough
		default:
			t.Fatalf("field %s of kind %s: teach the test", v.Type().Field(i).Name, f.Kind())
		}
	}
	return r
}

func TestEveryFieldOfARowIsDecidedAsKeptOrDropped(t *testing.T) {
	all := reflect.TypeOf(ikhnosv1.RawFlow{})
	for i := 0; i < all.NumField(); i++ {
		name := all.Field(i).Name
		if !all.Field(i).IsExported() {
			continue
		}
		if !slices.Contains(keptFields, name) && !slices.Contains(droppedFields, name) {
			t.Errorf("RawFlow.%s is new: decide whether the link series are made of it (link/meter.go) and name it in keptFields or droppedFields", name)
		}
	}
	n := Narrow(filled(t))
	v := reflect.ValueOf(n).Elem()
	for _, name := range keptFields {
		if v.FieldByName(name).IsZero() {
			t.Errorf("Narrow drops %s, which is named as kept", name)
		}
	}
	for _, name := range droppedFields {
		if !v.FieldByName(name).IsZero() {
			t.Errorf("Narrow keeps %s, which is named as dropped", name)
		}
	}
}

func TestNarrowingKeepsEverythingTheSeriesAreMadeOf(t *testing.T) {
	cfg := serverConfig(t)
	full, narrow := link.NewMeter(link.NewPeers()), link.NewMeter(link.NewPeers())
	full.Peers.Set(cfg.ClusterId, cfg.LinkPeers)
	narrow.Peers.Set(cfg.ClusterId, cfg.LinkPeers)
	rows := []*ikhnosv1.RawFlow{filled(t), filled(t), row("10.30.0.9", nil), row("10.30.0.9", func(r *ikhnosv1.RawFlow) { r.Client = false }), row("10.20.0.6", func(r *ikhnosv1.RawFlow) { r.FailedAttempts, r.FailedRefused, r.FailedTimeout = 5, 2, 1 })}
	var narrowed []*ikhnosv1.RawFlow
	for _, r := range rows {
		narrowed = append(narrowed, Narrow(r))
	}
	for _, known := range []bool{true, false} {
		full.ObserveRows(rows, known)
		narrow.ObserveRows(narrowed, known)
	}
	if a, b := scrape(full), scrape(narrow); a != b || !strings.Contains(a, "ikhnos_link_connections_total") {
		t.Fatalf("the narrowed rows make other series than the whole rows:\n%s\n---\n%s", a, b)
	}
	if Narrow(nil) != nil {
		t.Error("Narrow(nil) must be nil")
	}
}

// ---- the messages ----

func TestTheConfigurationAndTheRowsMakeTheSeriesInTheHubThatTheAgentWouldHaveMade(t *testing.T) {
	r := newRig(t)
	cfg := serverConfig(t)
	r.f.SetConfig(cfg)
	rows := []*ikhnosv1.RawFlow{row("10.20.0.5", nil), row("10.30.0.9", func(x *ikhnosv1.RawFlow) { x.Client = false })}
	r.f.ObserveRows(rows, true)
	if err := r.cycle(false); err != nil {
		t.Fatal(err)
	}
	// The agent's own way, in its process.
	own := link.NewMeter(link.NewPeers())
	own.Peers.Set(cfg.ClusterId, cfg.LinkPeers)
	own.ObserveRows(rows, true)
	if a, b := scrape(own), scrape(r.meter); a != b {
		t.Fatalf("the hub's series are not the agent's:\n%s\n---\n%s", a, b)
	}
	st := r.f.Stats()
	if !st.InSync || !st.Reached || st.Hub.Peers != 2 || st.Sent != 2 || st.Queued != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if r.hub.Rows() != 2 {
		t.Errorf("rows = %d", r.hub.Rows())
	}
}

func TestTheHubIsToldTheSliceItNeedsAndNothingElse(t *testing.T) {
	cfg := serverConfig(t)
	s := Slice(cfg)
	if s.ApprovedAccessTier != 0 || len(s.PausedCollectors) != 0 || s.ClusterId != "cl-here" || len(s.LinkPeers) != 2 || s.LinkIperfServer == nil {
		t.Fatalf("slice = %v", s)
	}
	if got := Slice(nil); got == nil {
		t.Fatal("nil config must give an empty slice, not nil")
	}
	if tier := serverConfig(t); Digest(hubSecret, Slice(cfg)) != Digest(hubSecret, Slice(&ikhnosv1.Config{ClusterId: tier.ClusterId, ApprovedAccessTier: 1, LinkPeers: tier.LinkPeers, LinkIperfServer: tier.LinkIperfServer})) {
		t.Error("the digest of the slice must not depend on what the slice leaves out")
	}
	other := serverConfig(t)
	other.LinkPeers[1].Addresses = []string{"10.30.0.10"}
	if Digest(hubSecret, cfg) == Digest(hubSecret, other) {
		t.Error("another configuration has the same digest")
	}
	if Digest(hubSecret, cfg) == Digest([]byte("another-secret-0123456789abcdef"), cfg) {
		t.Error("the digest must depend on the secret")
	}
}

func TestWhatCrossesTheNetworkHoldsNoCredentialAndNoWorkload(t *testing.T) {
	r := newRig(t)
	r.f.SetConfig(serverConfig(t))
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil)}, true)
	if err := r.cycle(false); err != nil {
		t.Fatal(err)
	}
	if len(r.bodies) < 2 {
		t.Fatalf("%d messages", len(r.bodies))
	}
	priv, _ := keys(t)
	for i, b := range r.bodies {
		s := string(b)
		for _, mark := range []string{testPassword, "SECRETMARK", "PRIVATE KEY", strings.Split(priv, "\n")[1], "cl-here", "Athens", "payments.internal.example", "db.shop.svc", "eth0"} {
			if strings.Contains(s, mark) {
				t.Errorf("message %d carries %q in the clear", i, mark)
			}
		}
	}
}

func TestOnlyTheAgentMaySendAndNothingMaySendTwice(t *testing.T) {
	r := newRig(t)
	r.hub.Apply(serverConfig(t))
	body, _ := protojson.Marshal(&ikhnosv1.FlowReport{BytesKnown: true, Flows: []*ikhnosv1.RawFlow{row("10.20.0.5", nil)}})
	send := func(secret []byte, node, ctx, path string, ts int64, b []byte) int {
		t := strconv.FormatInt(ts, 10)
		req, _ := http.NewRequest("POST", r.srv.URL+path, bytes.NewReader(b))
		req.Header.Set(probe.HeaderNode, node)
		req.Header.Set(probe.HeaderTime, t)
		req.Header.Set(probe.HeaderSig, probe.SignContext(ctx, secret, t, node, b))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	now := time.Now().Unix()
	if c := send(hubSecret, AgentNode, rowsContext, PathRows, now, body); c != 200 {
		t.Fatalf("the agent: %d", c)
	}
	if c := send(hubSecret, AgentNode, rowsContext, PathRows, now, body); c != http.StatusConflict {
		t.Errorf("a replay: %d, want 409", c)
	}
	if c := send([]byte("another-secret-0123456789abcdef"), AgentNode, rowsContext, PathRows, now+1, body); c != http.StatusUnauthorized {
		t.Errorf("another secret: %d", c)
	}
	if c := send(hubSecret, "link-prober", rowsContext, PathRows, now+2, body); c != http.StatusBadRequest {
		t.Errorf("another node name: %d", c)
	}
	// The prober's and the responder's contexts, with the right secret, are not the agent's.
	if c := send(hubSecret, AgentNode, "ikhnos-link-peers-v1", PathRows, now+3, body); c != http.StatusUnauthorized {
		t.Errorf("the prober's context: %d", c)
	}
	if c := send(hubSecret, AgentNode, configContext, PathRows, now+4, body); c != http.StatusUnauthorized {
		t.Errorf("the configuration's context on the rows' path: %d", c)
	}
	if c := send(hubSecret, AgentNode, rowsContext, PathRows, now-3600, body); c != http.StatusUnauthorized {
		t.Errorf("an hour old: %d", c)
	}
	// A configuration that is not sealed under the hub's secret and the message's time does not open.
	if c := send(hubSecret, AgentNode, configContext, PathConfig, now+5, []byte("not sealed at all, not even close")); c != http.StatusBadRequest {
		t.Errorf("an unsealed configuration: %d", c)
	}
	plain := []byte(`{"v":1,"config":{}}`)
	wrongTime, _ := link.Seal(hubSecret, configSeal, strconv.FormatInt(now+99, 10), plain)
	if c := send(hubSecret, AgentNode, configContext, PathConfig, now+6, wrongTime); c != http.StatusBadRequest {
		t.Errorf("a configuration sealed for another time: %d", c)
	}
	otherVersion, _ := link.Seal(hubSecret, configSeal, strconv.FormatInt(now+7, 10), []byte(`{"v":2,"config":{}}`))
	if c := send(hubSecret, AgentNode, configContext, PathConfig, now+7, otherVersion); c != http.StatusBadRequest {
		t.Errorf("another version: %d", c)
	}
	if got := len(r.hub.Meter.Peers.List()); got != 2 {
		t.Errorf("a refused message changed the peers: %d", got)
	}
}

func TestTooManyRowsInOneMessageAreRefused(t *testing.T) {
	r := newRig(t)
	r.hub.Apply(serverConfig(t))
	rows := make([]*ikhnosv1.RawFlow, MaxRowsPerPost+1)
	for i := range rows {
		rows[i] = &ikhnosv1.RawFlow{PeerIp: "10.20.0.5"}
	}
	body, _ := protojson.Marshal(&ikhnosv1.FlowReport{Flows: rows})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest("POST", r.srv.URL+PathRows, bytes.NewReader(body))
	req.Header.Set(probe.HeaderNode, AgentNode)
	req.Header.Set(probe.HeaderTime, ts)
	req.Header.Set(probe.HeaderSig, probe.SignContext(rowsContext, hubSecret, ts, AgentNode, body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// ---- the two sides failing on their own ----

func TestAHubThatWasRestartedIsToldTheConfigurationAgain(t *testing.T) {
	r := newRig(t)
	r.f.SetConfig(serverConfig(t))
	if err := r.cycle(true); err != nil {
		t.Fatal(err)
	}
	if !r.f.Stats().InSync || len(r.meter.Peers.List()) != 2 {
		t.Fatal("not in sync to begin with")
	}
	r.setHub() // a new process: it holds nothing
	if len(r.meter.Peers.List()) != 0 {
		t.Fatal("the new hub holds peers")
	}
	// Rows that arrive are refused by a hub that holds nothing, kept, and sent once the configuration has gone first, in the same cycle.
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil)}, true)
	if err := r.cycle(false); err != nil {
		t.Fatal(err)
	}
	if st := r.f.Stats(); !st.InSync || st.Queued != 0 || len(r.meter.Peers.List()) != 2 {
		t.Fatalf("the hub was not told again: %+v", st)
	}
	if got := scrape(r.meter); !strings.Contains(got, `ikhnos_link_connections_total{cluster="cl-here",peer="cl-a"} 3`) {
		t.Fatalf("the row that met a hub with no configuration was lost:\n%s", got)
	}
	// A hub that was restarted while nothing was being sent is noticed by the next empty message.
	r.setHub()
	if err := r.cycle(true); err != nil {
		t.Fatal(err)
	}
	if st := r.f.Stats(); !st.InSync || len(r.meter.Peers.List()) != 2 {
		t.Fatalf("an idle restart was not noticed: %+v", st)
	}
}

func TestRowsWaitForAHubThatCannotBeReachedAndAreDeliveredAfterwards(t *testing.T) {
	r := newRig(t)
	r.f.SetConfig(serverConfig(t))
	if err := r.cycle(false); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.down = true
	r.mu.Unlock()
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil), row("10.30.0.9", nil)}, true)
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil)}, false)
	if err := r.cycle(false); err == nil {
		t.Fatal("a hub that is down answered")
	}
	if st := r.f.Stats(); st.Queued != 3 || st.Sent != 0 {
		t.Fatalf("the rows were lost: %+v", st)
	}
	r.mu.Lock()
	r.down = false
	r.mu.Unlock()
	for i := 0; i < 3; i++ {
		if err := r.cycle(false); err != nil {
			t.Fatal(err)
		}
	}
	if st := r.f.Stats(); st.Queued != 0 || st.Sent != 3 || st.Dropped != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if got := scrape(r.meter); !strings.Contains(got, `ikhnos_link_connections_total{cluster="cl-here",peer="cl-a"} 6`) {
		t.Fatalf("what waited was not counted:\n%s", got)
	}
}

func TestWhatWaitsForAHubIsBoundedAndTheOldestIsDropped(t *testing.T) {
	f := NewForwarder("http://127.0.0.1:1", hubSecret, nil, nil)
	batch := make([]*ikhnosv1.RawFlow, 1000)
	for i := range batch {
		batch[i] = row("10.20.0.5", func(r *ikhnosv1.RawFlow) { r.Connections = uint64(i + 1) })
	}
	for i := 0; i < MaxQueuedRows/1000+5; i++ {
		f.ObserveRows(batch, true)
	}
	st := f.Stats()
	if st.Queued != MaxQueuedRows || st.Dropped != 5000 {
		t.Fatalf("queued %d, dropped %d", st.Queued, st.Dropped)
	}
	f.ObserveRows(nil, true)
	var nilF *Forwarder
	nilF.ObserveRows(batch, true)
	nilF.SetConfig(nil)
	if nilF.Stats().Reached || !nilF.ProberSeen().IsZero() {
		t.Error("a nil forwarder must say nothing")
	}
}

func TestTheRowsOfTheAgentsOwnAddressesAndTheProbersPortsNeverLeaveTheAgent(t *testing.T) {
	r := newRig(t)
	filter, err := link.NewRowFilter("8861,18900-18999", "10.42.0.99")
	if err != nil {
		t.Fatal(err)
	}
	r.f.filter = filter
	r.f.SetConfig(serverConfig(t))
	r.f.ObserveRows([]*ikhnosv1.RawFlow{
		row("10.20.0.5", nil),
		row("10.20.0.5", func(x *ikhnosv1.RawFlow) { x.Port = 8861 }),
		row("10.20.0.5", func(x *ikhnosv1.RawFlow) { x.Port = 18950 }),
		row("10.20.0.5", func(x *ikhnosv1.RawFlow) { x.LocalIp = "10.42.0.99" }),
	}, true)
	if st := r.f.Stats(); st.Queued != 1 {
		t.Fatalf("queued %d, want only the workload's row", st.Queued)
	}
	if _, err := link.NewRowFilter("eight", ""); err == nil {
		t.Error("a port that is not a port was accepted")
	}
	if _, err := link.NewRowFilter("", "not-an-address"); err == nil {
		t.Error("an address that is not an address was accepted")
	}
	if !(*link.RowFilter)(nil).Keep(row("10.20.0.5", nil)) || (*link.RowFilter)(nil).Keep(nil) {
		t.Error("a nil filter keeps every row and no nil")
	}
}

// ---- the pods that ask the hub ----

func TestTheProberAndTheRespondersAskTheHubWhatTheyAskedTheAgent(t *testing.T) {
	r := newRig(t)
	cfg := serverConfig(t)
	r.f.SetConfig(cfg)
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil)}, true)
	if err := r.cycle(false); err != nil {
		t.Fatal(err)
	}
	pods := httptest.NewServer(r.meter.Handler())
	defer pods.Close()
	ctx := context.Background()

	doc, err := link.FetchPeersWith(ctx, pods.Client(), pods.URL, []byte("peers-secret-0123456789abcdef"), time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ClusterID != "cl-here" || len(doc.Peers) != 2 || doc.Peers[0].ID != "cl-a" || doc.Peers[0].BytesSent != 1000 {
		t.Fatalf("peers = %+v", doc)
	}
	if doc.Peers[0].Iperf == nil || doc.Peers[0].Iperf.Username != testUser || doc.Peers[0].Iperf.Password.Reveal() != testPassword {
		t.Errorf("the prober did not get its client credentials: %+v", doc.Peers[0].Iperf)
	}
	if !slices.Contains(doc.Peers[0].Preferred, "10.20.0.5") {
		t.Errorf("the address the traffic uses is not preferred: %v", doc.Peers[0].Preferred)
	}
	srv, err := link.FetchIperfServer(ctx, pods.Client(), pods.URL, []byte("iperf-secret-0123456789abcdef"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := keys(t)
	if srv.PrivateKeyPEM.Reveal() != priv || srv.UsersCSV.Reveal() != usersCSV() {
		t.Error("the responder did not get the server credentials")
	}
	// The prober's last request is what the agent reports to the server, learned on the next message.
	if !r.f.ProberSeen().IsZero() {
		t.Error("the forwarder knew of the prober before the hub's next answer")
	}
	if err := r.cycle(true); err != nil {
		t.Fatal(err)
	}
	if got := r.f.ProberSeen(); time.Since(got) > time.Minute || got.IsZero() {
		t.Errorf("prober seen = %v", got)
	}
	// Each of the two pods has a secret of its own, and none is the agent's.
	if _, err := link.FetchPeers(ctx, pods.Client(), pods.URL, hubSecret, time.Now()); err == nil {
		t.Error("the agent's secret was accepted as the prober's")
	}
	if _, err := link.FetchIperfServer(ctx, pods.Client(), pods.URL, []byte("peers-secret-0123456789abcdef"), time.Now()); err == nil {
		t.Error("the prober's secret was accepted as the responder's")
	}
}

func TestAHubThatHasNotBeenToldAnythingSaysSoToThePodsThatAskIt(t *testing.T) {
	r := newRig(t)
	pods := httptest.NewServer(r.meter.Handler())
	defer pods.Close()
	doc, err := link.FetchPeers(context.Background(), pods.Client(), pods.URL, []byte("peers-secret-0123456789abcdef"), time.Now())
	if err != nil || doc.ClusterID != "" || len(doc.Peers) != 0 {
		t.Fatalf("doc %+v err %v: an empty answer with no cluster id is how the prober knows it was not told yet", doc, err)
	}
	if _, err := link.FetchIperfServer(context.Background(), pods.Client(), pods.URL, []byte("iperf-secret-0123456789abcdef"), time.Now()); err != link.ErrIperfNotReady {
		t.Errorf("err = %v, want ErrIperfNotReady: a responder must keep what it has", err)
	}
}

func TestRunSendsOnItsOwnAndStops(t *testing.T) {
	r := newRig(t)
	r.f.SetConfig(serverConfig(t))
	r.f.ObserveRows([]*ikhnosv1.RawFlow{row("10.20.0.5", nil)}, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.f.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(r.f.Stats().InSync && r.f.Stats().Sent == 1 && len(r.meter.Peers.List()) == 2) {
		time.Sleep(20 * time.Millisecond)
	}
	if st := r.f.Stats(); !st.InSync || st.Sent != 1 {
		t.Fatalf("stats = %+v", st)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
