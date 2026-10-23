package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// LowerAITaskBudget lowers the spend cap of one chat, never raises it:
// `aitask budget lower --task ID --usd N`. scope is conversation or task (the
// global, kind and role caps are Settings-only, through SetAITaskBudget); the
// CLI lowers the cap of a task's conversation, so the key of a conversation
// scope that names no task is refused (conversation_needs_task) and the key of
// a task scope is the task id. What is plainly wrong (a scope outside the two,
// a non-finite or negative value, a value not strictly below the cap the task
// shows now) is refused here without a CLI call; the CLI stays the authority
// on everything else and its refusal is returned verbatim.
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
	if scope == "conversation" {
		return lowerRefusal("conversation_needs_task", "this chat has no task of its own whose cap can be lowered from here")
	}
	var cur struct {
		CapUsd float64 `json:"capUsd"`
		Error  string  `json:"error"`
	}
	if json.Unmarshal([]byte(a.jsonResult("aitask", "budget", "show", "--task="+key)), &cur) == nil && cur.Error == "" && cur.CapUsd > 0 && usd >= cur.CapUsd {
		return lowerRefusal("cap_raise_refused", fmt.Sprintf("a cap can only be lowered here: %v USD is not below the current %v USD", usd, cur.CapUsd))
	}
	return a.jsonResult("aitask", "budget", "lower", "--task="+key, "--usd="+strconv.FormatFloat(usd, 'f', -1, 64))
}

func lowerRefusal(code, msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg, "code": code})
	return string(b)
}
