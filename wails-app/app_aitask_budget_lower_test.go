//go:build !windows

package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func lowerCode(t *testing.T, raw string) string {
	t.Helper()
	var r struct{ Error, Code string }
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return r.Code
}

func lowerCalls(t *testing.T, log string) []string {
	var got []string
	for _, l := range readArgsLog(t, log) {
		got = append(got, strings.TrimPrefix(l, "--profile default --json "))
	}
	return got
}

// argv is exact: --flag=value, so a key that looks like a flag stays one value.
func TestApp_LowerAITaskBudget_ArgvExact(t *testing.T) {
	bin, log := chatFakeCLI(t,
		fakeChatReply{match: "aitask budget show --task", stdout: `{"taskId":"x","capUsd":5,"spentUsd":1,"reported":true}` + "\n"},
		fakeChatReply{match: "aitask budget lower", stdout: `{"taskId":"x","capUsd":2.5}` + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	if got := a.LowerAITaskBudget("task", "--usd=99", 2.5); !strings.Contains(got, `"capUsd":2.5`) {
		t.Fatalf("result = %s", got)
	}
	want := "aitask budget show --task=--usd=99|aitask budget lower --task=--usd=99 --usd=2.5"
	if got := strings.Join(lowerCalls(t, log), "|"); got != want {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

// Refused before any CLI call: other scopes, non-finite, negative, not below the cap, conversation without a task.
func TestApp_LowerAITaskBudget_RefusedWithoutCLI(t *testing.T) {
	bin, log := chatFakeCLI(t,
		fakeChatReply{match: "aitask budget show --task", stdout: `{"taskId":"x","capUsd":5,"spentUsd":1,"reported":true}` + "\n"},
		fakeChatReply{match: "aitask budget lower", stdout: `{}` + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	for _, c := range []struct {
		name, scope, key string
		usd              float64
		code             string
	}{
		{"global", "global", "", 1, "scope_refused"},
		{"kind", "kind", "generic", 1, "scope_refused"},
		{"role", "role", "writer", 1, "scope_refused"},
		{"empty key", "task", "", 1, "invalid_input"},
		{"NaN", "task", "x", math.NaN(), "invalid_input"},
		{"+Inf", "task", "x", math.Inf(1), "invalid_input"},
		{"negative", "task", "x", -1, "invalid_input"},
		{"conversation", "conversation", "c1", 1, "conversation_needs_task"},
	} {
		if got := lowerCode(t, a.LowerAITaskBudget(c.scope, c.key, c.usd)); got != c.code {
			t.Errorf("%s: code = %q, want %q", c.name, got, c.code)
		}
	}
	if calls := lowerCalls(t, log); len(calls) != 0 {
		t.Errorf("CLI called for refused input: %q", calls)
	}
	// Equal and higher than the shown cap: one show, no lower.
	for _, usd := range []float64{5, 7} {
		if got := lowerCode(t, a.LowerAITaskBudget("task", "x", usd)); got != "cap_raise_refused" {
			t.Errorf("usd %v: code = %q", usd, got)
		}
	}
	for _, c := range lowerCalls(t, log) {
		if strings.Contains(c, "lower") {
			t.Errorf("lower reached the CLI: %q", c)
		}
	}
}

// The CLI stays the authority: its refusal comes back with its code.
func TestApp_LowerAITaskBudget_CLIRefusalMapped(t *testing.T) {
	bin, _ := chatFakeCLI(t,
		fakeChatReply{match: "aitask budget show --task", code: 3, stdout: `{"error":"unknown task","code":"not_found"}` + "\n"},
		fakeChatReply{match: "aitask budget lower", code: 3, stderr: "x\n", stdout: `{"error":"nothing to lower","code":"cap_raise_refused"}` + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	if got := lowerCode(t, a.LowerAITaskBudget("task", "x", 1)); got != "cap_raise_refused" {
		t.Errorf("code = %q", got)
	}
}
