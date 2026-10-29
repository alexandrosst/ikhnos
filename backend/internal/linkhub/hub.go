package linkhub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/link"
	"ikhnos/internal/probe"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Hub is the receiving side: the meter that makes the series and answers the prober and the responders (package link serves those on its
// own listener), and the two messages the agent feeds it with.
type Hub struct {
	Meter *link.Meter

	secret []byte
	replay *probe.ReplayCache
	limit  *probe.SourceLimit
	log    *slog.Logger
	now    func() time.Time

	mu     sync.Mutex
	digest string
	rows   uint64
}

// New makes a hub that takes the agent's messages signed with secret, into m. m is the meter whose handler is served on the metrics listener.
func New(secret []byte, m *link.Meter, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	// One agent sends, a few times a second at the most; a source that sends far more than that is not the agent.
	return &Hub{Meter: m, secret: append([]byte(nil), secret...), replay: probe.NewReplayCache(0), limit: probe.NewSourceLimit(250*time.Millisecond, 40, 64), log: log, now: time.Now}
}

// Status is what the hub holds, as the answers carry it.
func (h *Hub) Status() Status {
	h.mu.Lock()
	d := h.digest
	h.mu.Unlock()
	st := Status{Version: Version, ConfigDigest: d, Peers: len(h.Meter.Peers.List())}
	if t := h.Meter.ProberSeen(); !t.IsZero() {
		st.ProberSeenUnix = t.Unix()
	}
	return st
}

// Rows is how many rows the hub has taken in since it started.
func (h *Hub) Rows() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rows
}

// Digest identifies a configuration: a MAC of its deterministic encoding under the secret, so that it says nothing about what is in it and
// cannot be computed by anyone who does not hold the secret.
func Digest(secret []byte, c *ikhnosv1.Config) string {
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(c)
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(digestLabel + "\n"))
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// Slice is the part of a Config the hub is given: the cluster, its peers and the iperf3 credentials. Anything else in the Config the agent holds
// is not copied, so there is nothing else to leak and nothing else for a hub to be told to do.
func Slice(c *ikhnosv1.Config) *ikhnosv1.Config {
	if c == nil {
		return &ikhnosv1.Config{}
	}
	return &ikhnosv1.Config{ClusterId: c.ClusterId, LinkPeers: c.LinkPeers, LinkIperfServer: c.LinkIperfServer}
}

// Apply puts a configuration in force the way the agent put it in force itself: the peers, the credentials, and the forgetting of the state of
// peers that are gone. It returns what it dropped and why.
func (h *Hub) Apply(c *ikhnosv1.Config) (notes []string) {
	notes = h.Meter.Peers.Set(c.ClusterId, c.LinkPeers)
	if why := h.Meter.SetIperfServer(c.LinkIperfServer); why != "" {
		notes = append(notes, "the iperf3 server credentials were refused: "+why)
	}
	h.Meter.Forget()
	h.mu.Lock()
	h.digest = Digest(h.secret, c)
	h.mu.Unlock()
	return notes
}

// Handler serves the two messages. It is a listener of its own, reached by the agent only.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathRows, h.rowsHandler)
	mux.HandleFunc("POST "+PathConfig, h.configHandler)
	return mux
}

func (h *Hub) read(w http.ResponseWriter, req *http.Request, context string, max int64) ([]byte, string, bool) {
	_, body, status, err := probe.ReadSigned(w, req, probe.Signed{Secret: h.secret, MaxBody: max, Replay: h.replay, Limit: h.limit, NodeOK: func(n string) bool { return n == AgentNode }, Now: h.now(), Context: context})
	if err != nil {
		if status == http.StatusUnauthorized || status == http.StatusConflict {
			h.log.Warn("rejected a message for the link hub", "reason", err.Error(), "from", probe.SourceOf(req))
		}
		if status == http.StatusTooManyRequests && h.limit.Note(h.now()) {
			h.log.Warn("turned away messages for the link hub: one source sends more than the agent does", "from", probe.SourceOf(req))
		}
		http.Error(w, http.StatusText(status), status)
		return nil, "", false
	}
	return body, req.Header.Get(probe.HeaderTime), true
}

func (h *Hub) answer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(h.Status())
}

func (h *Hub) rowsHandler(w http.ResponseWriter, req *http.Request) {
	body, _, ok := h.read(w, req, rowsContext, maxRowsBody)
	if !ok {
		return
	}
	st := h.Status()
	if st.ConfigDigest == "" {
		// Rows before any configuration would be looked up among no peers and counted for none. The agent is told, and sends the configuration first.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(st)
		return
	}
	var rep ikhnosv1.FlowReport
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, &rep); err != nil || len(rep.Flows) > MaxRowsPerPost {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Whatever the agent says, the meter takes in what it can use and counts nothing it cannot: a row without a peer address is ignored there.
	h.Meter.ObserveRows(rep.Flows, rep.BytesKnown)
	h.mu.Lock()
	h.rows += uint64(len(rep.Flows))
	h.mu.Unlock()
	h.answer(w)
}

func (h *Hub) configHandler(w http.ResponseWriter, req *http.Request) {
	body, ts, ok := h.read(w, req, configContext, maxConfigBody)
	if !ok {
		return
	}
	plain, err := link.Open(h.secret, configSeal, ts, body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer clear(plain)
	var doc configDoc
	if err := json.Unmarshal(plain, &doc); err != nil || doc.Version != Version {
		h.log.Warn("a configuration for the link hub of another version or unreadable", "version", doc.Version)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var c ikhnosv1.Config
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(doc.Config, &c); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for _, n := range h.Apply(Slice(&c)) {
		h.log.Warn("link configuration", "note", n)
	}
	h.answer(w)
}

// configDoc is the plain text of a configuration message, before it is sealed.
type configDoc struct {
	Version int             `json:"v"`
	Config  json.RawMessage `json:"config"`
}
