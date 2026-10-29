package probe

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestASourceMayBurstThenIsHeldToTheRate(t *testing.T) {
	now := time.Now()
	l := NewSourceLimit(2*time.Second, 3, 100)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("10.0.0.1", now); !ok {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	ok, wait := l.Allow("10.0.0.1", now)
	if ok || wait != 2*time.Second {
		t.Fatalf("a fourth at once: ok %v, wait %v", ok, wait)
	}
	// Another source has a bucket of its own.
	if ok, _ := l.Allow("10.0.0.2", now); !ok {
		t.Fatal("one source's flood used up another's")
	}
	// One token comes back every two seconds, and no more than the burst ever accumulates.
	if ok, _ := l.Allow("10.0.0.1", now.Add(1500*time.Millisecond)); ok {
		t.Fatal("allowed before a token was back")
	}
	if ok, _ := l.Allow("10.0.0.1", now.Add(2*time.Second)); !ok {
		t.Fatal("refused after a token was back")
	}
	later := now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("10.0.0.1", later); !ok {
			t.Fatalf("after an hour, %d of the burst was refused", i+1)
		}
	}
	if ok, _ := l.Allow("10.0.0.1", later); ok {
		t.Fatal("an hour of quiet saved up more than the burst")
	}
	if l.Refused() != 3 {
		t.Fatalf("refused = %d, want 3", l.Refused())
	}
}

func TestTheSourcesRememberedAreBounded(t *testing.T) {
	now := time.Now()
	l := NewSourceLimit(time.Second, 2, 50)
	for i := 0; i < 500; i++ {
		l.Allow("10.1."+strconv.Itoa(i/250)+"."+strconv.Itoa(i%250), now.Add(time.Duration(i)*time.Millisecond))
	}
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n > 50 {
		t.Fatalf("%d sources remembered, at most 50", n)
	}
	// Sources that are quiet again are the ones forgotten, so a busy source keeps its bucket.
	busy := NewSourceLimit(time.Minute, 2, 3)
	busy.Allow("busy", now)
	busy.Allow("busy", now)
	for _, s := range []string{"a", "b", "c"} {
		busy.Allow(s, now.Add(time.Second))
	}
	if ok, _ := busy.Allow("busy", now.Add(2*time.Second)); ok {
		t.Fatal("a source that had used its burst got a new one by the others crowding the table")
	}
}

func TestANilLimitAllowsEverythingAndNoteSpeaksOncePerMinute(t *testing.T) {
	var none *SourceLimit
	if ok, _ := none.Allow("x", time.Now()); !ok || none.Refused() != 0 || none.Note(time.Now()) {
		t.Fatal("a missing limit must allow everything and say nothing")
	}
	l := DefaultSourceLimit()
	now := time.Now()
	if !l.Note(now) || l.Note(now.Add(59*time.Second)) || !l.Note(now.Add(61*time.Second)) {
		t.Fatal("a refusal is to be logged at most once a minute")
	}
}

// failingBody fails the test if anything reads it.
type failingBody struct{ t *testing.T }

func (b failingBody) Read([]byte) (int, error) {
	b.t.Helper()
	b.t.Error("the body of a request from a source over its rate was read")
	return 0, errors.New("must not be read")
}
func (failingBody) Close() error { return nil }

func TestARequestOverTheRateIsAnsweredBeforeItIsRead(t *testing.T) {
	now := time.Now()
	l := NewSourceLimit(time.Second, 1, 10)
	s := Signed{Secret: []byte("0123456789abcdef"), MaxBody: 1 << 10, Now: now, Limit: l}
	first := httptest.NewRequest("POST", "/", strings.NewReader("x"))
	first.RemoteAddr = "10.9.9.9:4000"
	if _, _, status, _ := ReadSigned(httptest.NewRecorder(), first, s); status != http.StatusBadRequest && status != http.StatusUnauthorized {
		t.Fatalf("the first request was not judged on its own: %d", status)
	}
	second := httptest.NewRequest("POST", "/", failingBody{t})
	second.RemoteAddr = "10.9.9.9:4001" // another connection from the same address
	w := httptest.NewRecorder()
	_, _, status, err := ReadSigned(w, second, s)
	if status != http.StatusTooManyRequests || !errors.Is(err, ErrRate) {
		t.Fatalf("status %d, err %v", status, err)
	}
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q", got)
	}
}

func TestAFlowReportIsNotANodeProbeReportWhateverTheSecret(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	r := NewReceiver(secret, nil)
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	// Posted the way a flow collector posts, to the probe's receiver, with the very same secret.
	if _, err := PostSignedContext(t.Context(), srv.Client(), srv.URL+PathReport, "ikhnos-flow-v1", secret, "n1", []byte(`{"probeVersion":"1"}`), time.Now()); err == nil {
		t.Fatal("a signature made for the flow receiver was accepted by the node probe's")
	}
	if _, err := PostSigned(t.Context(), srv.Client(), srv.URL+PathReport, secret, "n1", []byte(`{"probeVersion":"1"}`), time.Now()); err != nil {
		t.Fatalf("its own context was refused: %v", err)
	}
}
