package fusion

import (
	"context"
	"time"
)

const (
	// How long the stores may take to start before the switch says something is wrong: pulling three images and
	// binding volumes is slow on a fresh node, but not this slow.
	fusionStartGrace = 5 * time.Minute
	// With a fault that will not clear by itself (an image that cannot be pulled, a volume that cannot be bound) there
	// is no point waiting the full grace; this is long enough for a pull or a provisioner to finish a normal job.
	fusionHardGrace = 90 * time.Second

	// The workloads are read at most this often however many screens, tokens and health checks ask: they all share one
	// read (see statusWithin).
	fusionStatusTTL = 2500 * time.Millisecond
	// A FUSION access token's status call is answered from what the last read found, up to this old.
	fusionTokenStatusAge = time.Minute
	// Each Kubernetes call, and the whole read, has its own limit, so a slow API server cannot hold a request open.
	fusionKubeTimeout = 4 * time.Second
	fusionReadTimeout = 10 * time.Second
	// Prometheus is asked for the time of its newest sample no more often than this.
	fusionLastDataEvery = 15 * time.Second
)

// statusCache is the last read of the workloads and the read in progress, if any.
type statusCache struct {
	st     FusionStatus
	at     time.Time
	valid  bool
	gen    uint64 // bumped when the cluster is changed from here: a read that began before it must not be kept
	flight *statusFlight
}

type statusFlight struct {
	done chan struct{}
	st   FusionStatus
}

func (f *FusionControl) statusTTL() time.Duration {
	if f.StatusTTL == 0 {
		return fusionStatusTTL
	}
	return max(f.StatusTTL, 0)
}

// Status is the switch's state. Reads of the cluster are shared: one per fusionStatusTTL however many ask at once, and
// anyone who arrives while one is under way waits for it instead of starting another. A change made from here (Enable,
// Disable) replaces what is kept.
func (f *FusionControl) Status(ctx context.Context) FusionStatus {
	if f == nil {
		return f.statusWithin(ctx, 0)
	}
	return f.statusWithin(ctx, f.statusTTL())
}

// TokenStatus is Status for a FUSION access token's call: a token holder may poll as fast as the rate limit allows, so
// it is answered from the last read unless that is a minute old.
func (f *FusionControl) TokenStatus(ctx context.Context) FusionStatus {
	if f == nil {
		return f.statusWithin(ctx, 0)
	}
	if f.statusTTL() == 0 {
		return f.statusWithin(ctx, 0)
	}
	return f.statusWithin(ctx, fusionTokenStatusAge)
}

func (f *FusionControl) statusWithin(ctx context.Context, maxAge time.Duration) FusionStatus {
	if f == nil || f.Kube == nil {
		return FusionStatus{State: StateOff, Reason: ReasonNotConfigured, Message: "This server cannot switch FUSION: it was installed without it, or without the permission to scale it (fusion.enabled and fusionControl.enabled in the server chart)."}
	}
	if maxAge <= 0 {
		return f.readStatus(ctx)
	}
	f.cacheMu.Lock()
	if f.cache.valid && f.Clock().Sub(f.cache.at) < maxAge {
		st := f.cache.st.clone()
		f.cacheMu.Unlock()
		return st
	}
	if fl := f.cache.flight; fl != nil {
		f.cacheMu.Unlock()
		select {
		case <-fl.done:
			return fl.st.clone()
		case <-ctx.Done():
			return FusionStatus{Available: true, State: StateAttention, Message: "The Kubernetes API did not answer in time."}
		}
	}
	fl := &statusFlight{done: make(chan struct{}), st: FusionStatus{Available: true, State: StateAttention, Message: "The Kubernetes API did not answer in time."}}
	f.cache.flight = fl
	gen := f.cache.gen
	f.cacheMu.Unlock()

	// The read belongs to everyone waiting on it, not to the request that happened to start it. However it ends - a panic
	// included - the flight is closed, or everyone who joined it (and every later caller, who would join it too) would wait
	// for ever.
	read := false
	defer func() {
		f.cacheMu.Lock()
		if read && f.cache.gen == gen {
			f.cache.st, f.cache.at, f.cache.valid = fl.st.clone(), f.Clock(), true
		}
		if f.cache.flight == fl { // not one a later change already disowned (see invalidate)
			f.cache.flight = nil
		}
		close(fl.done)
		f.cacheMu.Unlock()
	}()
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fusionReadTimeout)
	defer cancel()
	fl.st = f.readStatus(rctx)
	read = true
	return fl.st.clone()
}

// invalidate drops what is kept and disowns any read already under way: the cluster is about to change.
func (f *FusionControl) Invalidate() {
	f.cacheMu.Lock()
	f.cache.valid = false
	f.cache.gen++
	f.cache.flight = nil // a read under way began before the change: nobody asking from now on may join it and be told the old state
	f.cacheMu.Unlock()
}

// keep stores a status the switch worked out itself, after a change it has just made.
func (f *FusionControl) keep(st FusionStatus) {
	f.cacheMu.Lock()
	f.cache.gen++
	f.cache.flight = nil // as in invalidate: what a read under way is about to find is older than this
	f.cache.st, f.cache.at, f.cache.valid = st.clone(), f.Clock(), true
	f.cacheMu.Unlock()
}

// CacheValid is whether the cached status can be served right now. A test reads it to see that a change in progress dropped it.
func (f *FusionControl) CacheValid() bool {
	f.cacheMu.Lock()
	defer f.cacheMu.Unlock()
	return f.cache.valid
}
