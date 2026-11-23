package fusionapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client reads the three stores. Each field is a store's base URL with no trailing slash ("" = that store is not
// configured, which every call reports as unavailable).
type Client struct {
	Prometheus string
	Loki       string
	Tempo      string
	HTTP       *http.Client
	// StoreTimeout bounds one call to a store (default 10 s), shorter than a request's own budget, so a store that hangs
	// costs the read that part and not the parts the other stores already answered.
	StoreTimeout time.Duration
	// MaxBytes bounds one store answer; a bigger one is refused rather than held in memory. Default 16 MiB.
	MaxBytes int64
	// Now is the clock, for tests.
	Now func() time.Time

	// semOnce/sem cap how many store calls are in flight at once for this Client, so a few fused reads cannot
	// crowd the stores out for everyone else.
	semOnce sync.Once
	sem     chan struct{}
	// bulkSem is the lane bulk reads (see WithBulk) pass through before the shared one, so they can never hold more than
	// maxBulkUpstream of the maxUpstream slots and a single read always finds room.
	bulkSem chan struct{}
}

const (
	// maxUpstream is the most store calls one Client has in flight at a time.
	maxUpstream = 8
	// maxBulkUpstream is the most of them a bulk read may hold.
	maxBulkUpstream = 5
)

const (
	defaultMaxBytes     = 16 << 20
	defaultStoreTimeout = 10 * time.Second
)

// DefaultMaxBytes and DefaultStoreTimeout are the bounds of one call to a store, unless the Client sets its own (the API's
// description states them).
const (
	DefaultMaxBytes     = defaultMaxBytes
	DefaultStoreTimeout = defaultStoreTimeout
)

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// storeTransport is shared by the clients that were given none. The default transport keeps two idle connections to a host, and a fused
// read asks one store from several places at once, so each read beyond the second would open a new connection.
var storeTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = 16
	return t
}()

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: storeTransport}
}

func (c *Client) storeTimeout() time.Duration {
	if c.StoreTimeout > 0 {
		return c.StoreTimeout
	}
	return defaultStoreTimeout
}

func (c *Client) maxBytes() int64 {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return defaultMaxBytes
}

// Store names, for messages.
const (
	storeProm  = "Prometheus"
	storeLoki  = "Loki"
	storeTempo = "Tempo"
)

// acquire takes one slot of sem, waiting for it or for ctx; release gives it back. Every place that bounds how many
// store calls run at once uses it, so the wait and the way out are the same everywhere.
func acquire(ctx context.Context, sem chan struct{}) (release func(), err error) {
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errf(http.StatusGatewayTimeout, "the time limit ran out before the stores could be asked")
		}
		return nil, ctx.Err()
	}
}

// lanes creates the in-flight limits of this Client on first use.
func (c *Client) lanes() {
	c.semOnce.Do(func() {
		c.sem = make(chan struct{}, maxUpstream)
		c.bulkSem = make(chan struct{}, maxBulkUpstream)
	})
}

// get calls one store and decodes a JSON answer into out (a *json.RawMessage keeps it verbatim).
func (c *Client) get(ctx context.Context, store, base, path string, q url.Values, hdr map[string]string, out any) error {
	if base == "" {
		return errf(http.StatusServiceUnavailable, "%s is not configured on this server", store)
	}
	c.lanes()
	if isBulk(ctx) { // wait in the bulk lane first, so a queue of bulk calls never sits in the shared one
		release, err := acquire(ctx, c.bulkSem)
		if err != nil {
			return err
		}
		defer release()
	}
	release, err := acquire(ctx, c.sem)
	if err != nil {
		return err
	}
	defer release()
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	callCtx, cancel := context.WithTimeout(ctx, c.storeTimeout()) // (after the wait for a slot, which is not the store's doing)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, u, nil)
	if err != nil {
		return errf(http.StatusInternalServerError, "could not build the %s request", store)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		if err := tooSlow(ctx, callCtx, store); err != nil {
			return err
		}
		return errf(http.StatusServiceUnavailable, "%s is not reachable. FUSION is off or still starting", store)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes()+1))
	if err != nil {
		if err := tooSlow(ctx, callCtx, store); err != nil {
			return err
		}
		return errf(http.StatusBadGateway, "%s: the answer was cut off", store)
	}
	if int64(len(body)) > c.maxBytes() {
		return errf(http.StatusUnprocessableEntity, "%s: the answer is larger than this API returns (%d MiB). Narrow the time range or the filters", store, c.maxBytes()>>20)
	}
	switch {
	case resp.StatusCode/100 == 2:
		if out == nil {
			return nil
		}
		if raw, ok := out.(*json.RawMessage); ok {
			*raw = body
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return errf(http.StatusBadGateway, "%s answered with something that is not JSON", store)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return errf(http.StatusNotFound, "%s: not found", store)
	case resp.StatusCode == http.StatusTooManyRequests:
		return errf(http.StatusTooManyRequests, "%s is busy; try again shortly", store)
	case resp.StatusCode/100 == 4:
		return errf(http.StatusBadRequest, "%s refused the query: %s", store, upstreamMessage(body))
	default:
		// A store's own error text can name hosts, paths and internals; the caller gets the fact, not the text.
		return errf(http.StatusBadGateway, "%s had a problem answering (status %d)", store, resp.StatusCode)
	}
}

// tooSlow says why a call that failed ran out of time, or nil when it did not. The caller going away is its own context's
// error; a deadline, the call's own or the request's, is a 504 naming the store, so that part of a fused read degrades to
// an error source and the rest is still returned.
func tooSlow(ctx, callCtx context.Context, store string) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	if ctx.Err() != nil || callCtx.Err() != nil {
		return errf(http.StatusGatewayTimeout, "%s took too long to answer; narrow the time range or the filters", store)
	}
	return nil
}

// upstreamMessage pulls the human part out of a store's error body (Prometheus and Loki answer JSON with "error",
// Tempo plain text) and keeps it short.
func upstreamMessage(body []byte) string {
	var j struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &j) == nil {
		if j.Error != "" {
			msg = j.Error
		} else if j.Message != "" {
			msg = j.Message
		}
	}
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	return msg
}

// IsUnavailable reports whether err is a store that could not be reached (FUSION off or starting), as opposed to a
// bad request or a store error. The fused views treat it as a missing part rather than a failure.
func IsUnavailable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusServiceUnavailable
}
