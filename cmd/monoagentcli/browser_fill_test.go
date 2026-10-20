package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
)

func formRunCalls(bt *browserTest) []extension.Command {
	var out []extension.Command
	for _, c := range bt.fake.Calls() {
		if c.Type == extension.CmdFormRun {
			out = append(out, c)
		}
	}
	return out
}

func answerFormRun(bt *browserTest) {
	bt.fake.On(extension.CmdFormRun, func(c *extension.Command) (any, error) {
		if c.Params["what"] == "files" {
			return map[string]any{"ok": true, "attached": 1, "found": 2}, nil
		}
		return map[string]any{"ok": true, "filled": 3, "found": 4, "generated": 1, "sources": []string{"cv.pdf"}}, nil
	})
}

func TestBrowserFillRunsTheFormFillerOnLocalPages(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", true)
	answerFormRun(bt)
	res := bt.runJSON("fill", "--generate")
	if res["filled"] != float64(3) || res["generated"] != float64(1) || res["found"] != float64(4) {
		t.Fatalf("result = %v", res)
	}
	calls := formRunCalls(bt)
	if len(calls) != 1 || calls[0].Params["profile"] != "default" || calls[0].Params["what"] != "fill" || calls[0].Params["generate"] != true {
		t.Fatalf("form_run calls = %v", calls)
	}
	res = bt.runJSON("fill", "--files")
	if res["attached"] != float64(1) || formRunCalls(bt)[1].Params["what"] != "files" || formRunCalls(bt)[1].Params["generate"] != nil {
		t.Fatalf("files result = %v %v", res, formRunCalls(bt)[1].Params)
	}
	if _, err := bt.run("fill", "--files", "--generate"); exitCodeFor(err) != 3 {
		t.Fatalf("files with generate: %v", err)
	}
}

func TestBrowserFillWaitsForApprovalOnRealSites(t *testing.T) {
	bt := newBrowserTest(t, true)
	db := bt.db().DB
	if err := browserctl.SaveAllowlist(db, []string{"https://jobs.example"}); err != nil {
		t.Fatal(err)
	}
	tab := bt.fake.AddTab("https://jobs.example/apply", "Coder · Agent", true)
	answerFormRun(bt)
	out, err := bt.run("fill", "--generate")
	if exitCodeFor(err) != 3 || !strings.Contains(out, "awaiting_approval") || !strings.Contains(out, "browser fill --hil") {
		t.Fatalf("held fill: %v %s", err, out)
	}
	if len(formRunCalls(bt)) != 0 {
		t.Fatal("the held fill reached the page")
	}
	id := strings.Split(strings.Split(out, `"hil_id": "`)[1], `"`)[0]
	t.Setenv("CLAUDECODE", "")
	if _, err := runHIL(t, bt.cfg, "approve", id); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")

	// The other two held kinds do not take it.
	for _, verb := range []string{"submit", "upload"} {
		if _, err := bt.run(verb, "--hil", id); err == nil {
			t.Fatalf("a fill approval was used by %s", verb)
		}
		if it, _ := browserctl.GetSubmitItem(t.Context(), db, "default", id); it.Status != "approved" {
			t.Fatalf("%s: the approval was not handed back: %s", verb, it.Status)
		}
	}
	// Another page of the same site is not the approved one.
	bt.fake.SetURL(tab, "https://jobs.example/other")
	if _, err := bt.run("fill", "--hil", id); exitCodeFor(err) != 3 || jsonCode(err) != "submit_changed" {
		t.Fatalf("fill on another page: %v (%s)", err, jsonCode(err))
	}
	bt.fake.SetURL(tab, "https://jobs.example/apply")
	res := bt.runJSON("fill", "--hil", id)
	if res["filled"] != float64(3) {
		t.Fatalf("approved fill: %v", res)
	}
	if calls := formRunCalls(bt); len(calls) != 1 || calls[0].Params["generate"] != true {
		t.Fatalf("form_run calls = %v", calls)
	}
	if _, err := bt.run("fill", "--hil", id); exitCodeFor(err) != 3 {
		t.Fatalf("one approval allowed a second fill: %v", err)
	}
}

func TestBrowserFillAtFullNoApprovals(t *testing.T) {
	bt := newBrowserTest(t, true)
	db := bt.db().DB
	if err := browserctl.SaveAllowlist(db, []string{"https://jobs.example"}); err != nil {
		t.Fatal(err)
	}
	bt.fake.AddTab("https://jobs.example/apply", "Coder · Agent", true)
	answerFormRun(bt)
	if _, err := browserctl.UpdatePermissions(db, func(p *browserctl.Permissions) error {
		return p.SetLevel(browserctl.KindOther, browserctl.LevelFullNoApprovals)
	}); err != nil {
		t.Fatal(err)
	}
	if res := bt.runJSON("fill"); res["filled"] != float64(3) {
		t.Fatalf("agent at full-no-approvals: %v", res)
	}
}
