package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// LowerAITaskBudget lowers the spend cap of one chat, never raises it: `aitask
// budget lower --task=ID --usd=N` (scope task, key = task id) or `--conversation=ID
// --usd=N` (scope conversation, key = conversation id, which creates the chat's
// own cap when only the global one applied). The global, kind and role caps are
// Settings-only, through SetAITaskBudget. What is plainly wrong (a scope outside
// the two, a non-finite or negative value, a value not strictly below the cap
// that shows now) is refused here without a lowering call; the CLI stays the
// authority on everything else and its refusal is returned verbatim.
func (a *App) LowerAITaskBudget(scope, key string, usd float64) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	if scope != "conversation" && scope != "task" {
		return lowerRefusal("scope_refused", "only a chat's own cap can be lowered here; other caps are set in Settings")
	}
	if key == "" {
		return lowerRefusal("invalid_input", "which chat or task is missing")
	}
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd < 0 {
		return lowerRefusal("invalid_input", "a cap is a number of USD, 0 or more (0 means no spend)")
	}
	flag := "--task="
	if scope == "conversation" {
		flag = "--conversation="
	}
	if cur, ok := a.shownCap(scope, key); ok && usd >= cur {
		return lowerRefusal("cap_raise_refused", fmt.Sprintf("a cap can only be lowered here: %v USD is not below the current %v USD", usd, cur))
	}
	return a.jsonResult("aitask", "budget", "lower", flag+key, "--usd="+strconv.FormatFloat(usd, 'f', -1, 64))
}

// shownCap is the cap the CLI shows for a task (`show --task`) or the lowest
// that applies to a conversation (the global cap, its own and the coder budget
// of `show`); ok is false when none is known.
func (a *App) shownCap(scope, key string) (float64, bool) {
	if scope == "task" {
		var cur struct {
			CapUsd float64 `json:"capUsd"`
			Error  string  `json:"error"`
		}
		if json.Unmarshal([]byte(a.jsonResult("aitask", "budget", "show", "--task="+key)), &cur) != nil || cur.Error != "" || cur.CapUsd <= 0 {
			return 0, false
		}
		return cur.CapUsd, true
	}
	var rep struct {
		OperatorUsd float64 `json:"operatorUsd"`
		Error       string  `json:"error"`
		Caps        []struct {
			Scope  string  `json:"scope"`
			Key    string  `json:"key"`
			CapUsd float64 `json:"capUsd"`
		} `json:"caps"`
	}
	if json.Unmarshal([]byte(a.jsonResult("aitask", "budget", "show")), &rep) != nil || rep.Error != "" {
		return 0, false
	}
	best, ok := 0.0, false
	take := func(v float64) {
		if !ok || v < best {
			best, ok = v, true
		}
	}
	if rep.OperatorUsd > 0 {
		take(rep.OperatorUsd)
	}
	for _, c := range rep.Caps {
		if c.Scope == "global" || (c.Scope == "conversation" && c.Key == key) {
			take(c.CapUsd)
		}
	}
	return best, ok
}

func lowerRefusal(code, msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg, "code": code})
	return string(b)
}
