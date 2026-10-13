package workflow

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"
)

func noSleep(t *testing.T) {
	t.Helper()
	old := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { retrySleep = old })
}

func TestExecuteWithRetry_NoTimeoutByDefault(t *testing.T) {
	ex := funcExecutor(func(ctx context.Context, _ NodeInput) ([]NodeOutput, error) {
		if _, ok := ctx.Deadline(); ok {
			t.Error("attempt context has a deadline without node_timeout")
		}
		return nil, nil
	})
	if _, err := executeWithRetry(context.Background(), ex, NodeInput{}, map[string]interface{}{}, RetryPolicy{}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteWithRetry_NodeTimeoutIsClassifiedAndRetried(t *testing.T) {
	noSleep(t)
	calls := 0
	ex := funcExecutor(func(ctx context.Context, _ NodeInput) ([]NodeOutput, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	})
	cfg := map[string]interface{}{"node_timeout": 0.02}
	_, err := executeWithRetry(context.Background(), ex, NodeInput{}, cfg, RetryPolicy{MaxRetries: 2, InitialDelay: 1})
	if !errors.Is(err, ErrNodeTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want ErrNodeTimeout wrapping DeadlineExceeded", err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3 (timeouts are retryable)", calls)
	}
}

func TestExecuteWithRetry_NodeTimeoutSuccessWithinLimit(t *testing.T) {
	ex := funcExecutor(func(ctx context.Context, _ NodeInput) ([]NodeOutput, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("no deadline with node_timeout set")
		}
		return []NodeOutput{{Handle: "main"}}, nil
	})
	for _, v := range []interface{}{5, 5.0, "5"} {
		if _, err := executeWithRetry(context.Background(), ex, NodeInput{}, map[string]interface{}{"node_timeout": v}, RetryPolicy{}); err != nil {
			t.Fatalf("node_timeout=%#v: %v", v, err)
		}
	}
}

func TestExtractNodeTimeout(t *testing.T) {
	cases := map[string]struct {
		v    interface{}
		want time.Duration
	}{
		"unset":    {nil, 0},
		"zero":     {0.0, 0},
		"negative": {-3.0, 0},
		"float":    {1.5, 1500 * time.Millisecond},
		"int":      {2, 2 * time.Second},
		"string":   {"3", 3 * time.Second},
		"junk":     {"soon", 0},
	}
	for name, c := range cases {
		cfg := map[string]interface{}{}
		if c.v != nil {
			cfg["node_timeout"] = c.v
		}
		if got := extractNodeTimeout(cfg); got != c.want {
			t.Errorf("%s: got %s want %s", name, got, c.want)
		}
	}
}

func items(vals ...string) []Item {
	out := make([]Item, len(vals))
	for i, v := range vals {
		out[i] = NewItem(map[string]interface{}{"v": v})
	}
	return out
}

func values(outs []NodeOutput) []string {
	var vs []string
	for _, o := range outs {
		for _, it := range o.Items {
			vs = append(vs, it.JSON["v"].(string))
		}
	}
	sort.Strings(vs)
	return vs
}

// partialExecutor fails the items whose value is in failing[attempt] (by
// value, so it is independent of the subset positions) and succeeds the rest.
func partialExecutor(t *testing.T, failing []map[string]error, seen *[]NodeInput) funcExecutor {
	return func(_ context.Context, in NodeInput) ([]NodeOutput, error) {
		attempt := len(*seen)
		*seen = append(*seen, in)
		var fail map[string]error
		if attempt < len(failing) {
			fail = failing[attempt]
		}
		pe := &PartialItemsError{}
		var ok []Item
		for i, it := range in.Items {
			if err, bad := fail[it.JSON["v"].(string)]; bad {
				pe.FailedIndexes = append(pe.FailedIndexes, i)
				pe.Errs = append(pe.Errs, err)
				continue
			}
			ok = append(ok, it)
		}
		if len(pe.FailedIndexes) == 0 {
			return []NodeOutput{{Handle: "main", Items: ok}}, nil
		}
		pe.Succeeded = []NodeOutput{{Handle: "main", Items: ok}}
		return nil, pe
	}
}

func TestExecuteWithRetry_PartialItemsRetriesOnlyFailed(t *testing.T) {
	noSleep(t)
	transient := errors.New("503")
	var seen []NodeInput
	ex := partialExecutor(t, []map[string]error{
		{"b": transient, "c": transient},
		{"c": transient},
	}, &seen)
	in := NodeInput{Items: items("a", "b", "c"), ExecutionID: "e1", NodeID: "n1"}
	var observed [][]string
	out, err := executeWithRetry(context.Background(), ex, in, nil, RetryPolicy{MaxRetries: 3, InitialDelay: 1},
		func(o []NodeOutput) { observed = append(observed, values(o)) })
	if err != nil {
		t.Fatal(err)
	}
	if got := values(out); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("merged outputs %v", got)
	}
	if len(seen) != 3 || len(seen[1].Items) != 2 || len(seen[2].Items) != 1 {
		t.Fatalf("attempt sizes: %d attempts", len(seen))
	}
	// Retried items keep the key of their original position.
	if seen[1].ItemKey(0) != in.ItemKey(1) || seen[1].ItemKey(1) != in.ItemKey(2) || seen[2].ItemKey(0) != in.ItemKey(2) {
		t.Fatal("retried items lost their original idempotency keys")
	}
	if seen[0].ItemKey(0) == seen[0].ItemKey(1) {
		t.Fatal("distinct items share a key")
	}
	want := [][]string{{"a"}, {"b"}, {"c"}}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("observer saw %v, want each attempt's outputs %v", observed, want)
	}
}

func TestExecuteWithRetry_PartialItemsFinalFailureKeepsSuccesses(t *testing.T) {
	noSleep(t)
	transient := errors.New("503")
	perm := Permanent(errors.New("404"))
	var seen []NodeInput
	ex := partialExecutor(t, []map[string]error{
		{"b": perm, "c": transient},
		{"c": transient},
		{"c": transient},
	}, &seen)
	in := NodeInput{Items: items("a", "b", "c")}
	out, err := executeWithRetry(context.Background(), ex, in, nil, RetryPolicy{MaxRetries: 2, InitialDelay: 1})
	var pe *PartialItemsError
	if !errors.As(err, &pe) {
		t.Fatalf("err=%v, want *PartialItemsError", err)
	}
	if !reflect.DeepEqual(pe.FailedIndexes, []int{1, 2}) || len(pe.Errs) != 2 || !errors.Is(pe.Errs[0], perm) || !errors.Is(pe.Errs[1], transient) {
		t.Fatalf("failed=%v errs=%v", pe.FailedIndexes, pe.Errs)
	}
	if got := values(out); !reflect.DeepEqual(got, []string{"a"}) || !reflect.DeepEqual(values(pe.Succeeded), got) {
		t.Fatalf("outputs=%v succeeded=%v", got, values(pe.Succeeded))
	}
	for i, s := range seen[1:] {
		if len(s.Items) != 1 || s.Items[0].JSON["v"] != "c" {
			t.Fatalf("retry %d ran %v; the permanent failure must not be retried", i+1, s.Items)
		}
	}
}

func TestExecuteWithRetry_WholeFailureAfterPartialKeepsSuccesses(t *testing.T) {
	noSleep(t)
	calls := 0
	ex := funcExecutor(func(_ context.Context, in NodeInput) ([]NodeOutput, error) {
		calls++
		if calls == 1 {
			return nil, &PartialItemsError{
				Succeeded:     []NodeOutput{{Handle: "main", Items: in.Items[:1]}},
				FailedIndexes: []int{1},
				Errs:          []error{errors.New("503")},
			}
		}
		return nil, errors.New("connection refused")
	})
	out, err := executeWithRetry(context.Background(), ex, NodeInput{Items: items("a", "b")}, nil, RetryPolicy{MaxRetries: 1, InitialDelay: 1})
	var pe *PartialItemsError
	if !errors.As(err, &pe) || !reflect.DeepEqual(pe.FailedIndexes, []int{1}) || values(out)[0] != "a" {
		t.Fatalf("out=%v err=%v", values(out), err)
	}
}

func TestExecuteWithRetry_PlainErrorUnchanged(t *testing.T) {
	noSleep(t)
	boom := errors.New("boom")
	out, err := executeWithRetry(context.Background(), funcExecutor(func(context.Context, NodeInput) ([]NodeOutput, error) {
		return []NodeOutput{{Handle: "main"}}, boom
	}), NodeInput{Items: items("a")}, nil, RetryPolicy{MaxRetries: 1, InitialDelay: 1})
	if out != nil || err != boom {
		t.Fatalf("out=%v err=%v, want nil and the node's own error", out, err)
	}
}
