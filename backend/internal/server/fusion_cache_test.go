package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Many callers that find nothing kept wait on one read, not one each; a caller who gives up stops waiting, and the read
// goes on for the others; a failure is the answer of those who waited and is not kept.
func TestASharedValueIsReadOnceForEveryoneWaiting(t *testing.T) {
	var c sharedValue[int]
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	var reads atomic.Int32
	release := make(chan struct{})
	read := func(context.Context) (int, error) {
		reads.Add(1)
		<-release
		return 7, nil
	}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(gone, clock, time.Second, 0, read); !errors.Is(err, context.Canceled) {
		t.Fatalf("a caller who gave up still waited: %v", err)
	}
	var wg sync.WaitGroup
	got := make([]int, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = c.get(context.Background(), clock, time.Second, 0, read)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads for one value", n)
	}
	for _, v := range got {
		if v != 7 {
			t.Fatalf("got %v", got)
		}
	}

	var f sharedValue[int]
	boom := errors.New("boom")
	if _, err := f.get(context.Background(), clock, time.Second, 0, func(context.Context) (int, error) { return 0, boom }); err != boom {
		t.Fatalf("the failure was not the answer: %v", err)
	}
	if v, err := f.get(context.Background(), clock, time.Second, 0, func(context.Context) (int, error) { return 3, nil }); err != nil || v != 3 {
		t.Fatalf("a failure was kept: %v %v", v, err)
	}
}

// With no stale allowance an old value is never given: the caller waits for the new one.
func TestASharedValueWithNoStaleAllowanceIsNeverGivenOld(t *testing.T) {
	var c sharedValue[int]
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c.at, c.val = now.Add(-2*time.Second), 1
	if v, _ := c.get(context.Background(), clock, time.Second, 0, func(context.Context) (int, error) { return 2, nil }); v != 2 {
		t.Fatalf("an old value was given: %d", v)
	}
}

// idle says no read of the value is in flight.
func (c *sharedValue[T]) idle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loading == nil
}
