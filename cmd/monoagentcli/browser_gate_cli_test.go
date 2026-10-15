package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
)

// An agent's `browser eval` could send a form around the approval a submit
// needs, so it is refused on allowlisted pages that are not local; local
// pages and a human's terminal are not restricted.
func TestBrowserEvalIsRefusedOnGatedPagesForAgents(t *testing.T) {
	bt := newBrowserTest(t, true)
	if err := browserctl.SaveAllowlist(bt.db().DB, []string{"https://shop.example"}); err != nil {
		t.Fatal(err)
	}
	shop := bt.fake.AddTab("https://shop.example/", "Coder · Agent", true)
	local := bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", false)
	if _, err := bt.run("eval", "--tab", itoa(shop), "fetch('/pay', {method: 'POST'})"); exitCodeFor(err) != 3 || jsonCode(err) != "eval_not_allowed" {
		t.Fatalf("agent eval on shop.example: %v (%s)", err, jsonCode(err))
	}
	for _, c := range bt.fake.Types() {
		if c == "eval_cdp" {
			t.Fatal("the refused script reached the page")
		}
	}
	if res := bt.runJSON("eval", "--tab", itoa(local), "1"); res["result"] == nil {
		t.Fatalf("agent eval on localhost: %v", res)
	}
	t.Setenv("CLAUDECODE", "")
	if _, err := bt.run("eval", "--tab", itoa(shop), "1"); err != nil {
		t.Fatalf("a human's eval on shop.example: %v", err)
	}
}

// After approval the form must still be what the person approved: changing
// a value refuses the submit (submit_changed) and hands the approval back.
func TestBrowserSubmitRefusesAChangedForm(t *testing.T) {
	bt := newBrowserTest(t, true)
	db := bt.db().DB
	if err := browserctl.SaveAllowlist(db, []string{"https://shop.example"}); err != nil {
		t.Fatal(err)
	}
	bt.fake.AddTab("https://shop.example/checkout", "Coder · Agent", true)
	amount := "10"
	bt.fake.On("eval_cdp", func(c *extension.Command) (any, error) {
		form := map[string]any{"action": "https://shop.example/pay", "method": "post", "selector": "#pay-form",
			"fields": []any{map[string]any{"name": "amount", "type": "text", "value": amount}}}
		if strings.Contains(c.Params["expression"].(string), "const el = document.querySelector") {
			return map[string]any{"result": map[string]any{"found": true, "submit": true, "why": "type=submit", "form": form}}, nil
		}
		return map[string]any{"result": map[string]any{"found": true, "form": form}}, nil
	})
	out, err := bt.run("click", "#pay")
	if exitCodeFor(err) != 3 || !strings.Contains(out, "awaiting_approval") {
		t.Fatalf("held click: %v %s", err, out)
	}
	id := strings.Split(strings.Split(out, `"hil_id": "`)[1], `"`)[0]
	t.Setenv("CLAUDECODE", "")
	if _, err := runHIL(t, bt.cfg, "approve", id); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")

	amount = "10000"
	if _, err := bt.run("submit", "--hil", id); exitCodeFor(err) != 3 || jsonCode(err) != "submit_changed" {
		t.Fatalf("submit after the amount changed: %v (%s)", err, jsonCode(err))
	}
	if it, _ := browserctl.GetSubmitItem(t.Context(), db, "default", id); it.Status != "approved" {
		t.Fatalf("the approval was not handed back: %s", it.Status)
	}
	amount = "10"
	if res := bt.runJSON("submit", "--hil", id); res["status"] != "submitted" {
		t.Fatalf("submit of the approved form: %v", res)
	}
}
