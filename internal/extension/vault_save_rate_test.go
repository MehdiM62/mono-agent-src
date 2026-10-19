package extension

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The first caller starts the read and gives up: it returns at once with its
// own cancellation, and the read carries on for the caller waiting on it.
func TestLiveProfileCache_CancelledCallerDoesNotFailOthers(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	c := newLiveProfileCache(func(ctx context.Context) ([]string, error) {
		if reads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return []string{"p-work"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, time.Now)

	first, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() { _, err := c.has(first, "p-work"); firstErr <- err }()
	<-started
	otherErr := make(chan error, 1)
	go func() {
		ok, err := c.has(context.Background(), "p-work")
		if err == nil && !ok {
			err = errors.New("p-work is not live")
		}
		otherErr <- err
	}()
	cancel()
	select {
	case err := <-firstErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the cancelled caller: %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the cancelled caller is still waiting on the read")
	}
	close(release)
	if err := <-otherErr; err != nil {
		t.Errorf("another caller got the first one's cancellation: %v", err)
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("%d reads, want 1", n)
	}
}

// A slow read is shared, not queued: five callers wait for one read, a
// caller with its own deadline returns at that deadline, and the read itself
// is bounded by liveProfileReadTimeout.
func TestLiveProfileCache_SlowReadHoldsNoCallerUp(t *testing.T) {
	const slow = 500 * time.Millisecond
	var reads atomic.Int32
	var left atomic.Int64
	c := newLiveProfileCache(func(ctx context.Context) ([]string, error) {
		reads.Add(1)
		if d, ok := ctx.Deadline(); ok {
			left.Store(int64(time.Until(d)))
		}
		time.Sleep(slow)
		return []string{"p-work"}, nil
	}, time.Now)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := c.has(context.Background(), "p-work"); err != nil || !ok {
				t.Errorf("a waiting caller: %v, %v", ok, err)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	if _, err := c.has(ctx, "p-work"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a caller past its own deadline: %v, want context.DeadlineExceeded", err)
	}
	if took := time.Since(t0); took >= slow*4/5 {
		t.Errorf("a caller with a 50 ms deadline waited %v, for the read", took)
	}
	wg.Wait()
	if took := time.Since(start); took >= 2*slow {
		t.Errorf("five callers of one %v read took %v", slow, took)
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("%d reads, want 1", n)
	}
	if l := time.Duration(left.Load()); l <= 0 || l > liveProfileReadTimeout {
		t.Errorf("the read had %v left, want a deadline within %v", l, liveProfileReadTimeout)
	}
}

// A read that panics is a failed read, not a crashed bridge: the read runs on
// its own goroutine, outside the request handler's recover.
func TestLiveProfileCache_PanickingReadIsAnError(t *testing.T) {
	c := newLiveProfileCache(func(context.Context) ([]string, error) { panic("boom") }, time.Now)
	if ok, err := c.has(context.Background(), "p-work"); ok || err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("has = %v, %v; want the panic as an error", ok, err)
	}
}

// A read that ignores its ctx (the production read opens the database, which
// takes none) holds no caller past its timeout plus liveProfileWaitMargin:
// eight callers get the list read before it, a cache with no list yet says
// unavailable, and the stuck read is still the only one in flight.
func TestLiveProfileCache_ReadIgnoringItsContextHoldsNoCallerPastTheTimeout(t *testing.T) {
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	var reads atomic.Int32
	read := func(context.Context) ([]string, error) {
		if reads.Add(1) > 1 {
			<-stuck // a locked database: the read's ctx is never looked at
		}
		return []string{"p-work"}, nil
	}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	c := newLiveProfileCache(read, func() time.Time { return now })
	c.timeout = 100 * time.Millisecond
	if ok, err := c.has(context.Background(), "p-work"); err != nil || !ok {
		t.Fatalf("the first read: %v, %v", ok, err)
	}
	now = now.Add(liveProfileTTL)

	bound := c.timeout + liveProfileWaitMargin
	type answer struct {
		ok   bool
		err  error
		took time.Duration
	}
	answers := make(chan answer, 8)
	for i := 0; i < 8; i++ {
		go func() {
			t0 := time.Now()
			ok, err := c.has(context.Background(), "p-work")
			answers <- answer{ok, err, time.Since(t0)}
		}()
	}
	for i := 0; i < 8; i++ {
		select {
		case a := <-answers:
			if a.err != nil || !a.ok {
				t.Errorf("caller %d: %v, %v; want the list read before (p-work live)", i, a.ok, a.err)
			}
			if a.took > bound+time.Second {
				t.Errorf("caller %d waited %v, bound %v", i, a.took, bound)
			}
		case <-time.After(bound + 2*time.Second):
			t.Fatalf("caller %d is still waiting on a read that ignores its ctx", i)
		}
	}
	if n := reads.Load(); n != 2 {
		t.Errorf("%d reads, want 2 (the stuck one is still the read in flight)", n)
	}

	// With no list read yet, the answer is unavailable, not a wait.
	empty := newLiveProfileCache(func(context.Context) ([]string, error) { <-stuck; return nil, nil }, time.Now)
	empty.timeout = 100 * time.Millisecond
	got := make(chan error, 1)
	go func() { _, err := empty.has(context.Background(), "p-work"); got <- err }()
	select {
	case err := <-got:
		var re *RequestError
		if !errors.As(err, &re) || re.Code != CodeUnavailable {
			t.Errorf("no list yet and the read stuck: %v, want unavailable", err)
		}
	case <-time.After(bound + 2*time.Second):
		t.Fatal("the caller of a cache with no list is still waiting on a read that ignores its ctx")
	}
}
