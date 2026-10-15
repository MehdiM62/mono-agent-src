package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/agentusage"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeTurns replaces monomind with reply, which answers each turn.
func fakeTurns(t *testing.T, reply func(opts monomind.ExecOptions) (*monomind.TurnResult, error)) {
	t.Helper()
	oldEnsure, oldExec := ensureMonomind, execTurn
	ensureMonomind = func(context.Context) (string, *monomind.VersionInfo, error) { return "monomind", nil, nil }
	execTurn = func(_ context.Context, opts monomind.ExecOptions, _ func(monomind.Event)) (*monomind.TurnResult, error) {
		return reply(opts)
	}
	t.Cleanup(func() { ensureMonomind, execTurn = oldEnsure, oldExec })
}

func items(n int) []workflow.Item {
	out := make([]workflow.Item, n)
	for i := range out {
		out[i] = workflow.Item{JSON: map[string]interface{}{"n": float64(i)}}
	}
	return out
}

const categorySchema = `{"type":"object","required":["category"],"properties":{"category":{"type":"string","enum":["lead","spam"]}}}`

func TestAskOutputSchemaParsesFencedJSONAndMerges(t *testing.T) {
	var prompt string
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		prompt = o.Prompt
		return &monomind.TurnResult{SessionID: "s1", ResultText: "Sure:\n```json\n{\"category\": \"lead\", \"score\": 3}\n```"}, nil
	})
	out, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "claude", "prompt": "classify", "output_schema": categorySchema, "merge_output": true, "output_key": "res",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "JSON Schema") {
		t.Errorf("prompt %q lacks the schema instruction", prompt)
	}
	got := out[0].Items[0].JSON
	res, ok := got["res"].(map[string]interface{})
	if !ok || res["category"] != "lead" || res["score"] != float64(3) {
		t.Errorf("res = %#v", got["res"])
	}
	if got["category"] != "lead" || got["n"] != float64(0) {
		t.Errorf("merged item = %#v", got)
	}
}

func TestAskOutputSchemaReasksInSameSession(t *testing.T) {
	var calls []monomind.ExecOptions
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		calls = append(calls, o)
		if len(calls) == 1 {
			return &monomind.TurnResult{SessionID: "s1", ResultText: `{"category":"maybe"}`}, nil
		}
		return &monomind.TurnResult{SessionID: "s1", ResultText: `{"category":"spam"}`}, nil
	})
	out, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "claude", "prompt": "classify",
		"output_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"category": map[string]interface{}{"enum": []interface{}{"lead", "spam"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].Resume != "s1" || !strings.Contains(calls[1].Prompt, "must be one of") {
		t.Fatalf("calls = %+v", calls)
	}
	if v := out[0].Items[0].JSON["agent_response"].(map[string]interface{}); v["category"] != "spam" {
		t.Errorf("agent_response = %#v", v)
	}
	if _, merged := out[0].Items[0].JSON["category"]; merged {
		t.Error("merged without merge_output")
	}
}

func TestAskOutputSchemaFreshTurnWhenResumeFails(t *testing.T) {
	var calls []monomind.ExecOptions
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		calls = append(calls, o)
		switch len(calls) {
		case 1:
			return &monomind.TurnResult{SessionID: "s1", ResultText: "no json here"}, nil
		case 2:
			return &monomind.TurnResult{Err: &monomind.ProtocolError{Code: "runner-error", Message: "cannot resume"}}, nil
		}
		return &monomind.TurnResult{ResultText: `["a","b"]`}, nil
	})
	out, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "codex", "prompt": "list", "output_schema": `{"type":"array","items":{"type":"string"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[2].Resume != "" || !strings.Contains(calls[2].Prompt, "list") || !strings.Contains(calls[2].Prompt, "rejected") {
		t.Fatalf("calls = %+v", calls)
	}
	if v := out[0].Items[0].JSON["agent_response"].([]interface{}); len(v) != 2 {
		t.Errorf("agent_response = %#v", v)
	}
}

func TestAskOutputSchemaSecondFailureFailsItem(t *testing.T) {
	n := 0
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		n++
		return &monomind.TurnResult{SessionID: "s1", ResultText: `{"other":1}`}, nil
	})
	_, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "claude", "prompt": "classify", "output_schema": categorySchema,
	})
	if err == nil || !strings.Contains(err.Error(), "after one retry") || !strings.Contains(err.Error(), `"category"`) {
		t.Fatalf("err = %v", err)
	}
	if n != 2 {
		t.Errorf("turns = %d, want 2", n)
	}
}

func TestAskOutputSchemaInvalidConfig(t *testing.T) {
	fakeTurns(t, func(monomind.ExecOptions) (*monomind.TurnResult, error) { return &monomind.TurnResult{}, nil })
	_, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "claude", "prompt": "x", "output_schema": "{not json",
	})
	if !errors.Is(err, workflow.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

// Without the new options the answer is the trimmed text, as before.
func TestAskPlainAnswerUnchanged(t *testing.T) {
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		if strings.Contains(o.Prompt, "JSON") || o.Resume != "" {
			t.Errorf("unexpected prompt/resume: %+v", o)
		}
		return &monomind.TurnResult{SessionID: "s", ResultText: "  hi  \n"}, nil
	})
	out, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{
		"runtime": "claude", "prompt": "hello {{$json.n}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out[0].Items[0].JSON
	if got["agent_response"] != "hi" || got["_agent_session_id"] != "s" || len(got) != 3 {
		t.Errorf("item = %#v", got)
	}
}

func TestAskConcurrencyKeepsOrder(t *testing.T) {
	var running, peak int32
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		cur := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return &monomind.TurnResult{ResultText: "r:" + o.Prompt}, nil
	})
	out, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(9)}, map[string]interface{}{
		"runtime": "claude", "prompt": "{{$json.n}}", "concurrency": float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, it := range out[0].Items {
		if it.JSON["agent_response"] != fmt.Sprintf("r:%d", i) {
			t.Errorf("item %d = %v", i, it.JSON["agent_response"])
		}
	}
	if peak < 2 || peak > 3 {
		t.Errorf("peak parallel turns = %d, want 2..3", peak)
	}
}

func TestAskConcurrencyFirstFailureStops(t *testing.T) {
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		if o.Prompt == "2" {
			return &monomind.TurnResult{Err: &monomind.ProtocolError{Code: "quota", Message: "usage limit reached"}}, nil
		}
		return &monomind.TurnResult{ResultText: "ok"}, nil
	})
	_, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(6)}, map[string]interface{}{
		"runtime": "claude", "prompt": "{{$json.n}}", "concurrency": float64(2),
	})
	if err == nil || !strings.Contains(err.Error(), "usage limit reached") {
		t.Fatalf("err = %v", err)
	}
}

func fakeRoster(t *testing.T, results []agentroster.Result, scan *monomind.ScanResult) {
	t.Helper()
	oldScan, oldList := scanRuntimes, listRoster
	scanRuntimes = func(context.Context) (*monomind.ScanResult, error) { return scan, nil }
	listRoster = func(context.Context, *sql.DB) ([]agentroster.Result, error) { return results, nil }
	t.Cleanup(func() { scanRuntimes, listRoster = oldScan, oldList })
}

func installed(ids ...string) *monomind.ScanResult {
	s := &monomind.ScanResult{}
	for _, id := range ids {
		s.Agents = append(s.Agents, monomind.ScanEntry{ID: id, Installed: true})
	}
	s.Agents = append(s.Agents, monomind.ScanEntry{ID: "absent"})
	return s
}

func TestAskAutoPicksValidatedModel(t *testing.T) {
	now := time.Now()
	fakeRoster(t, []agentroster.Result{
		{Runtime: "claude", Model: "opus", Status: agentroster.StatusOK, CostUSD: 0.05, HasCost: true, ValidatedAt: now},
		{Runtime: "codex", Model: "gpt-5", Status: agentroster.StatusOK, CostUSD: 0.01, HasCost: true, ValidatedAt: now},
		{Runtime: "qwen", Model: "q", Status: agentroster.StatusOK, CostUSD: 0.001, HasCost: true, ValidatedAt: now.Add(-30 * 24 * time.Hour)},
		{Runtime: "absent", Model: "x", Status: agentroster.StatusOK, CostUSD: 0, HasCost: true, ValidatedAt: now},
	}, installed("claude", "codex", "qwen"))
	var got monomind.ExecOptions
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		got = o
		return &monomind.TurnResult{ResultText: "ok"}, nil
	})
	ctx := vault.ContextWithDB(context.Background(), testdb.Open(t).DB)
	out, err := (&AskNode{}).Execute(ctx, workflow.NodeInput{Items: items(1)}, map[string]interface{}{"runtime": "auto", "prompt": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Runtime != "codex" || got.Model != "gpt-5" {
		t.Errorf("ran %s/%s, want codex/gpt-5", got.Runtime, got.Model)
	}
	if j := out[0].Items[0].JSON; j["_agent_runtime"] != "codex" || j["_agent_model"] != "gpt-5" {
		t.Errorf("item = %#v", j)
	}
}

func TestAskAutoFallsBackToInstalledRuntime(t *testing.T) {
	fakeRoster(t, nil, installed("kimicode", "claude"))
	var got monomind.ExecOptions
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		got = o
		return &monomind.TurnResult{ResultText: "ok"}, nil
	})
	if _, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{"runtime": "auto", "prompt": "x"}); err != nil {
		t.Fatal(err)
	}
	if got.Runtime != "kimicode" || got.Model != "" {
		t.Errorf("ran %s/%s, want kimicode", got.Runtime, got.Model)
	}
}

func TestAskAutoNothingInstalled(t *testing.T) {
	fakeRoster(t, nil, installed())
	fakeTurns(t, func(monomind.ExecOptions) (*monomind.TurnResult, error) {
		t.Error("ran a turn")
		return &monomind.TurnResult{}, nil
	})
	_, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, map[string]interface{}{"runtime": "auto", "prompt": "x"})
	if err == nil || !strings.Contains(err.Error(), "agent validate") {
		t.Fatalf("err = %v", err)
	}
}

func TestAskRecordsUsage(t *testing.T) {
	db := testdb.Open(t).DB
	var mu sync.Mutex
	n := 0
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return &monomind.TurnResult{SessionID: "s1", ResultText: "a", InputTokens: 10, HasInputTokens: true, OutputTokens: 2, HasOutputTokens: true, CostUSD: 0.003, HasCostUSD: true}, nil
		}
		return &monomind.TurnResult{SessionID: "s2", ResultText: "b"}, nil
	})
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "p1")
	_, err := (&AskNode{}).Execute(ctx, workflow.NodeInput{Items: items(2), WorkflowID: "wf", ExecutionID: "ex", NodeID: "nd"},
		map[string]interface{}{"runtime": "claude", "model": "sonnet", "prompt": "x"})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := agentusage.List(context.Background(), db, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("turns = %+v", turns)
	}
	a, b := turns[0], turns[1]
	if a.ProfileID != "p1" || a.WorkflowID != "wf" || a.ExecutionID != "ex" || a.NodeID != "nd" || a.Runtime != "claude" || a.Model != "sonnet" ||
		a.Source != agentusage.SourceAgentAsk || a.SessionID != "s1" || a.InputTokens == nil || *a.InputTokens != 10 || *a.OutputTokens != 2 || *a.CostUSD != 0.003 {
		t.Errorf("first turn = %+v", a)
	}
	if b.InputTokens != nil || b.OutputTokens != nil || b.CostUSD != nil {
		t.Errorf("second turn reported nothing but stored %+v", b)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:                              "object",
		"```json\n[1,2]\n```":                  "array",
		"Here it is: {\"a\": {\"b\": 2}} done": "object",
		"```\n{\"a\":1}\n```":                  "object",
	}
	for in, want := range cases {
		v, err := extractJSON(in)
		if err != nil || jsonType(v) != want {
			t.Errorf("extractJSON(%q) = %#v, %v; want %s", in, v, err, want)
		}
	}
	if _, err := extractJSON("nothing"); err == nil {
		t.Error("extractJSON(nothing) = nil error")
	}
}

func TestValidateSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object", "required": []interface{}{"n", "tags"},
		"properties": map[string]interface{}{
			"n":    map[string]interface{}{"type": "integer"},
			"tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"opt":  map[string]interface{}{"type": []interface{}{"string", "null"}},
		},
	}
	ok := map[string]interface{}{"n": float64(2), "tags": []interface{}{"x"}, "opt": nil}
	if err := validateSchema(schema, ok); err != nil {
		t.Errorf("valid: %v", err)
	}
	bad := []map[string]interface{}{
		{"tags": []interface{}{}},
		{"n": 1.5, "tags": []interface{}{}},
		{"n": float64(1), "tags": []interface{}{float64(3)}},
		{"n": float64(1), "tags": []interface{}{}, "opt": true},
	}
	for _, b := range bad {
		if err := validateSchema(schema, b); err == nil {
			t.Errorf("validateSchema(%v) = nil", b)
		}
	}
}
