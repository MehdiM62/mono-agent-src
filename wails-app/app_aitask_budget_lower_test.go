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

// Refused before any lowering call: other scopes, non-finite, negative, not below the cap.
func TestApp_LowerAITaskBudget_RefusedWithoutCLI(t *testing.T) {
	bin, log := chatFakeCLI(t,
		fakeChatReply{match: "aitask budget show --task", stdout: `{"taskId":"x","capUsd":5,"spentUsd":1,"reported":true}` + "\n"},
		fakeChatReply{match: "aitask budget lower", stdout: `{}` + "\n"},
		fakeChatReply{match: "aitask budget show", stdout: `{"operatorUsd":0,"caps":[{"scope":"global","key":"","capUsd":5},{"scope":"conversation","key":"c1","capUsd":3},{"scope":"conversation","key":"c2","capUsd":1}]}` + "\n"},
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
	} {
		if got := lowerCode(t, a.LowerAITaskBudget(c.scope, c.key, c.usd)); got != c.code {
			t.Errorf("%s: code = %q, want %q", c.name, got, c.code)
		}
	}
	if calls := lowerCalls(t, log); len(calls) != 0 {
		t.Errorf("CLI called for refused input: %q", calls)
	}
	// A conversation: the lowest of the global and its own cap applies (3 here).
	for _, usd := range []float64{3, 4} {
		if got := lowerCode(t, a.LowerAITaskBudget("conversation", "c1", usd)); got != "cap_raise_refused" {
			t.Errorf("conversation %v: code = %q", usd, got)
		}
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

// A conversation lowers through --conversation=, one value even when it looks like a flag.
func TestApp_LowerAITaskBudget_ConversationArgv(t *testing.T) {
	bin, log := chatFakeCLI(t,
		fakeChatReply{match: "aitask budget lower", stdout: `{"conversationId":"c","capUsd":1}` + "\n"},
		fakeChatReply{match: "aitask budget show", stdout: `{"operatorUsd":4,"caps":[{"scope":"global","key":"","capUsd":5}]}` + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	if got := a.LowerAITaskBudget("conversation", "--task=x", 1); !strings.Contains(got, `"capUsd":1`) {
		t.Fatalf("result = %s", got)
	}
	// The coder budget (4) counts as the cap that applies.
	if got := lowerCode(t, a.LowerAITaskBudget("conversation", "c", 4)); got != "cap_raise_refused" {
		t.Errorf("at the coder budget: code = %q", got)
	}
	want := "aitask budget show|aitask budget lower --conversation=--task=x --usd=1|aitask budget show"
	if got := strings.Join(lowerCalls(t, log), "|"); got != want {
		t.Errorf("calls = %q, want %q", got, want)
	}
}
