package probe

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// A receiver answers whoever can reach its port, and before it has checked a signature it has already read the headers and, for a request that
// passes the cheap checks, up to MaxBody of the body. A source that sends faster than a collector ever does is not a collector. SourceLimit is the
// first thing a request meets: a token bucket per source address, so one sender cannot keep the receiver reading and hashing, and the others
// still get through. It limits the address the connection came from, never the node name in a header, which any sender can write.
const (
	// DefaultSourceEvery and DefaultSourceBurst allow a source one request every two seconds on average and fifteen at once: a collector reports
	// every ten to thirty seconds and posts what it held back after an outage in one request, so this is far above what a collector needs.
	DefaultSourceEvery = 2 * time.Second
	DefaultSourceBurst = 15
	// DefaultSourceMax bounds the sources remembered.
	DefaultSourceMax = 4096
)

// SourceLimit is a token bucket per source. It is safe for concurrent use. A nil *SourceLimit allows everything.
type SourceLimit struct {
	every time.Duration
	burst float64
	max   int

	mu       sync.Mutex
	buckets  map[string]*bucket
	refused  uint64
	lastNote time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewSourceLimit allows burst requests at once and one more every `every`, for at most max sources at a time.
func NewSourceLimit(every time.Duration, burst, max int) *SourceLimit {
	if every <= 0 {
		every = DefaultSourceEvery
	}
	if burst < 1 {
		burst = 1
	}
	if max < 1 {
		max = DefaultSourceMax
	}
	return &SourceLimit{every: every, burst: float64(burst), max: max, buckets: map[string]*bucket{}}
}

// DefaultSourceLimit is the limit a receiver starts with.
func DefaultSourceLimit() *SourceLimit {
	return NewSourceLimit(DefaultSourceEvery, DefaultSourceBurst, DefaultSourceMax)
}

func (l *SourceLimit) refill(b *bucket, now time.Time) {
	if d := now.Sub(b.at); d > 0 {
		b.tokens = min(l.burst, b.tokens+float64(d)/float64(l.every))
	}
	b.at = now
}

// Allow takes one token from the source's bucket. When there is none it reports how long until there is.
func (l *SourceLimit) Allow(src string, now time.Time) (ok bool, retryAfter time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[src]
	if b == nil {
		if len(l.buckets) >= l.max {
			l.makeRoom(now)
		}
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[src] = b
	} else {
		l.refill(b, now)
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	l.refused++
	return false, time.Duration((1 - b.tokens) * float64(l.every))
}

// makeRoom forgets the sources whose bucket is full again (they hold nothing a new bucket would not) and, if every source is busy, the one
// with the most tokens left: the one that has been held to its rate least, so the least is lost.
func (l *SourceLimit) makeRoom(now time.Time) {
	var fullest string
	most := -1.0
	for k, b := range l.buckets {
		l.refill(b, now)
		if b.tokens >= l.burst {
			delete(l.buckets, k)
		} else if b.tokens > most {
			fullest, most = k, b.tokens
		}
	}
	if len(l.buckets) >= l.max && most >= 0 {
		delete(l.buckets, fullest)
	}
}

// Refused is how many requests were turned away since the limit was made.
func (l *SourceLimit) Refused() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refused
}

// Note reports whether a refusal should be logged now: at most once a minute, so a flood is one line, not a million.
func (l *SourceLimit) Note(now time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastNote) < time.Minute {
		return false
	}
	l.lastNote = now
	return true
}

// SourceOf is the address a request came from, without the port.
func SourceOf(req *http.Request) string {
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return host
	}
	return req.RemoteAddr
}
