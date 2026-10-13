package main

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Bubble asks (coder-bubble v2, Point 4): the bubbles show workflow
// human-in-loop items and the send-approval items of Point 3 as one card.
// These bindings only shell out to `monoagentcli hil …` and hand its JSON
// back untouched (unlike GetHILItems, whose typed struct would drop the
// fields send items carry). Every value goes after "--" so none can be read
// as a flag. Each returns the CLI's JSON or {"error": "..."}.

// HILList returns the pending HIL items exactly as `hil list` prints them.
func (a *App) HILList() string {
	var raw json.RawMessage
	if err := a.runMonoCLI("", &raw, "hil", "list"); err != nil {
		return aiError(err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	return string(raw)
}

// HILResolve approves or rejects one item. For an approval, dataJSON is a
// workflow item's edited fields and editText a send item's edited text; an
// edited send item is approved with {"text": editText} as its data.
func (a *App) HILResolve(id string, approve bool, editText, dataJSON string) string {
	if id == "" {
		return aiError(fmt.Errorf("hil: no item id"))
	}
	if !approve {
		if err := a.runMonoCLI("", nil, "hil", "reject", "--", id); err != nil {
			return aiError(err)
		}
		return `{"ok":true,"state":"rejected"}`
	}
	data := dataJSON
	if editText != "" {
		b, err := json.Marshal(map[string]string{"text": editText})
		if err != nil {
			return aiError(err)
		}
		data = string(b)
	}
	if data == "" {
		data = "{}"
	}
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(data), &check); err != nil {
		return aiError(fmt.Errorf("hil: edited data is not valid JSON: %w", err))
	}
	if err := a.runMonoCLI("", nil, "hil", "approve", "--data", data, "--", id); err != nil {
		return aiError(err)
	}
	return `{"ok":true,"state":"approved"}`
}

// HILAttachment returns attachment n of a send item for an inline preview
// (`hil attachment <id> <n>`): {dataUrl, mime, name, size}.
func (a *App) HILAttachment(id string, n int) string {
	if id == "" || n < 0 {
		return aiError(fmt.Errorf("hil: bad attachment reference"))
	}
	var raw json.RawMessage
	if err := a.runMonoCLI("", &raw, "hil", "attachment", "--", id, strconv.Itoa(n)); err != nil {
		return aiError(err)
	}
	return string(raw)
}
