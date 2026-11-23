package server

import (
	"context"
	"sync"
	"time"
)

// sharedReadTimeout bounds a read of a sharedValue made for many callers: it no longer ends with the request that
// started it, since the others wait on it too.
const sharedReadTimeout = 15 * time.Second

// sharedValue is something every request of this server needs and that is dear to work out (the applications, the
// names of the organisation and its clusters: each builds the whole state document), so it is worked out at most once at
// a time and reused for a while.
//
// A caller that finds it fresh takes it. One that finds it stale, but no staler than the cache allows, takes it too and
// starts the one read that renews it, so a slow read delays no caller that has an answer. One that finds nothing it may
// use waits for the read in flight, or starts it, for as long as its own request lasts: a caller who gives up stops
// waiting, and the read goes on for the others. A failed read is not kept; it is the answer of those who waited on it.
type sharedValue[T any] struct {
	mu      sync.Mutex
	at      time.Time // when val was read; zero when there is none
	val     T
	loading *sharedRead[T]
}

// sharedRead is one read in flight, and what it came to once done is closed.
type sharedRead[T any] struct {
	done chan struct{}
	val  T
	err  error
}

// get is the value, read with read when there is none fresh enough. now is the server's clock; ttl how long a value is
// fresh; stale how long past that it may still be given while a new one is read (0: never).
func (c *sharedValue[T]) get(ctx context.Context, now func() time.Time, ttl, stale time.Duration,
	read func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	age := now().Sub(c.at)
	if !c.at.IsZero() && age >= 0 && age < ttl+stale {
		v := c.val
		if age >= ttl && c.loading == nil {
			c.start(ctx, now, read)
		}
		c.mu.Unlock()
		return v, nil
	}
	l := c.loading
	if l == nil {
		l = c.start(ctx, now, read)
	}
	c.mu.Unlock()
	select {
	case <-l.done:
		return l.val, l.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// start begins a read for every caller; c.mu is held. The read keeps the values of the request that started it (whose
// organisation it is about) but not its end.
func (c *sharedValue[T]) start(ctx context.Context, now func() time.Time, read func(context.Context) (T, error)) *sharedRead[T] {
	l := &sharedRead[T]{done: make(chan struct{})}
	c.loading = l
	go func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedReadTimeout)
		defer cancel()
		v, err := read(rctx)
		c.mu.Lock()
		if err == nil {
			c.at, c.val = now(), v
		}
		c.loading = nil
		l.val, l.err = v, err
		c.mu.Unlock()
		close(l.done)
	}()
	return l
}
