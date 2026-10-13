package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// funcExecutor adapts a function to NodeExecutor for retry tests.
type funcExecutor func(ctx context.Context, in NodeInput) ([]NodeOutput, error)

func (funcExecutor) Type() string { return "test.func" }
func (f funcExecutor) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	return f(ctx, in)
}

// TestExecuteWithRetry_BackoffYieldsSlot runs MaxConcurrent executions that
// fail once and back off, then N more that only need a slot. The waiting ones
// must not hold the gate: the others finish during the back-off, the gate
// never admits more than MaxConcurrent active executions, and every token is
// returned at the end.
func TestExecuteWithRetry_BackoffYieldsSlot(t *testing.T) {
	const maxConcurrent, quick = 2, 3

	var active, peak atomic.Int32
	enter := func() {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				return
			}
		}
	}

	quickDone := make(chan struct{})
	var quickLeft atomic.Int32
	quickLeft.Store(quick)

	oldSleep := retrySleep
	retrySleep = func(ctx context.Context, d time.Duration) error {
		select {
		case <-quickDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("back-off held the slot: no other execution made progress")
		}
	}
	t.Cleanup(func() { retrySleep = oldSleep })

	inFirstAttempt := make(chan struct{}, maxConcurrent)
	results := make(chan error, maxConcurrent)
	handler := func(ctx context.Context, req ExecutionRequest) {
		if req.TriggerType != "backoff" {
			enter()
			defer active.Add(-1)
			if quickLeft.Add(-1) == 0 {
				close(quickDone)
			}
			return
		}
		var calls int
		ex := funcExecutor(func(context.Context, NodeInput) ([]NodeOutput, error) {
			enter()
			defer active.Add(-1)
			calls++
			if calls == 1 {
				inFirstAttempt <- struct{}{}
				return nil, errors.New("transient")
			}
			return []NodeOutput{{Handle: "main"}}, nil
		})
		_, err := executeWithRetry(ctx, ex, NodeInput{}, nil, RetryPolicy{MaxRetries: 1, InitialDelay: 1})
		results <- err
	}

	q := NewExecutionQueue(20, maxConcurrent, handler, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	for i := 0; i < maxConcurrent; i++ {
		if err := q.Enqueue(ExecutionRequest{ExecutionID: fmt.Sprint("b", i), TriggerType: "backoff"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < maxConcurrent; i++ {
		<-inFirstAttempt
	}
	for i := 0; i < quick; i++ {
		if err := q.Enqueue(ExecutionRequest{ExecutionID: fmt.Sprint("q", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < maxConcurrent; i++ {
		if err := <-results; err != nil {
			t.Fatalf("backing-off execution: %v", err)
		}
	}
	q.Stop()

	if p := peak.Load(); p > maxConcurrent {
		t.Fatalf("peak active executions %d > bound %d", p, maxConcurrent)
	}
	if n := len(q.gate.tokens); n != 0 {
		t.Fatalf("%d gate token(s) still held after all executions finished", n)
	}
}

// TestExecuteWithRetry_BackoffCancelledWhileWaitingForSlot: an execution whose
// back-off ends while another holds the only slot, and is then cancelled,
// returns ErrExecutionCancelled instead of running again.
func TestExecuteWithRetry_BackoffCancelledWhileWaitingForSlot(t *testing.T) {
	g := newGate(1)
	slot := &gateSlot{g: g}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := slot.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	ctx = ContextWithSlot(ctx, slot)

	other := &gateSlot{g: g}
	oldSleep := retrySleep
	retrySleep = func(context.Context, time.Duration) error {
		// The slot was yielded: someone else takes it, then the run is cancelled.
		actx, acancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer acancel()
		if err := other.Acquire(actx); err != nil {
			t.Error("slot not yielded during back-off:", err)
		}
		time.AfterFunc(20*time.Millisecond, cancel)
		return nil
	}
	t.Cleanup(func() { retrySleep = oldSleep })

	var calls int
	ex := funcExecutor(func(context.Context, NodeInput) ([]NodeOutput, error) {
		calls++
		return nil, errors.New("transient")
	})
	_, err := executeWithRetry(ctx, ex, NodeInput{}, nil, RetryPolicy{MaxRetries: 2, InitialDelay: 1})
	if !errors.Is(err, ErrExecutionCancelled) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want ErrExecutionCancelled after 1 call", err, calls)
	}
	slot.Release() // not held: must not free other's token
	if n := len(g.tokens); n != 1 {
		t.Fatalf("gate holds %d tokens, want 1 (other's)", n)
	}
}

func TestYieldSlotWhile(t *testing.T) {
	t.Run("no slot runs fn", func(t *testing.T) {
		ran := false
		if err := YieldSlotWhile(context.Background(), func() error { ran = true; return nil }); err != nil || !ran {
			t.Fatalf("err=%v ran=%v", err, ran)
		}
	})
	t.Run("released during fn and held after", func(t *testing.T) {
		g := newGate(1)
		slot := &gateSlot{g: g}
		ctx := ContextWithSlot(context.Background(), slot)
		_ = slot.Acquire(ctx)
		var during int
		want := errors.New("fn error")
		err := YieldSlotWhile(ctx, func() error { during = len(g.tokens); return want })
		if !errors.Is(err, want) || during != 0 || len(g.tokens) != 1 || !slot.held {
			t.Fatalf("err=%v during=%d after=%d held=%v", err, during, len(g.tokens), slot.held)
		}
	})
	t.Run("concurrent yields keep accounting", func(t *testing.T) {
		g := newGate(1)
		slot := &gateSlot{g: g}
		ctx := ContextWithSlot(context.Background(), slot)
		_ = slot.Acquire(ctx)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = YieldSlotWhile(ctx, func() error { return nil })
			}()
		}
		wg.Wait()
		if len(g.tokens) != 1 || !slot.held {
			t.Fatalf("tokens=%d held=%v, want 1/true", len(g.tokens), slot.held)
		}
	})
}
