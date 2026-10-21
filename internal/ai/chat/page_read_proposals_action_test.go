package chat

import (
	"strings"
	"testing"
)

func TestProposePageActionNamesOneStepAndDoesNothing(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	before := rowCount(t, db.DB)

	out, err := mt.Execute("propose_page_action", `{"action":"double_click","target":"  CV -\n Morteza  2025 Apr "}`)
	if err != nil {
		t.Fatal(err)
	}
	res, p := proposalOf(t, out)
	if p["kind"] != "action" || p["action"] != "double_click" || p["target"] != "CV - Morteza 2025 Apr" {
		t.Fatalf("proposal = %v", p)
	}
	if res["added"] != false {
		t.Fatalf("the answer must say nothing was done: %s", out)
	}

	out, err = mt.Execute("propose_page_action", `{"action":"type","target":"Search","text":"a\u0000b\nc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, p = proposalOf(t, out); p["text"] != "ab\nc" {
		t.Fatalf("control characters must be dropped from typed text: %v", p)
	}

	out, err = mt.Execute("propose_page_action", `{"action":"open_url","url":"https://example.com/a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, p = proposalOf(t, out); p["url"] != "https://example.com/a" || p["target"] != nil {
		t.Fatalf("open_url proposal = %v", p)
	}
	if after := rowCount(t, db.DB); after != before {
		t.Fatalf("a proposal wrote %d rows", after-before)
	}
}

func TestProposePageActionRefusesWhatItCannotName(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	for _, args := range []string{
		`{"action":"eval","target":"x"}`,
		`{"action":"click"}`,
		`{"action":"click","target":" \n "}`,
		`{"action":"type","target":"Search"}`,
		`{"action":"open_url","url":"javascript:alert(1)"}`,
		`{"action":"open_url","url":"https://a b"}`,
		`{"action":"open_url"}`,
		`{"action":"check"}`,
		`{"action":"select","target":"Country"}`,
		`{"action":"scroll","text":"sideways"}`,
		`{"action":"press_key","text":"Enter"}`,
		`{"action":"press_key","target":"Search"}`,
		`not json`,
	} {
		if _, err := mt.Execute("propose_page_action", args); err == nil {
			t.Errorf("propose_page_action accepted %s", args)
		}
	}
	out, err := mt.Execute("propose_page_action", `{"action":"click","target":"`+strings.Repeat("é", 500)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, p := proposalOf(t, out); len([]rune(p["target"].(string))) != actionTargetRunes {
		t.Errorf("target was not cut to %d runes", actionTargetRunes)
	}
}

func TestProposeCoderHandoffQueuesNothing(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	before := rowCount(t, db.DB)
	out, err := mt.Execute("propose_coder_handoff", `{"goal":" Fill the store listing\nfrom the docs ","why":"spans\n pages"}`)
	if err != nil {
		t.Fatal(err)
	}
	res, p := proposalOf(t, out)
	if p["kind"] != "coder" || p["goal"] != "Fill the store listing\nfrom the docs" || p["why"] != "spans pages" || res["added"] != false {
		t.Fatalf("proposal = %v", p)
	}
	if _, err := mt.Execute("propose_coder_handoff", `{"goal":"  "}`); err == nil {
		t.Error("an empty goal was accepted")
	}
	if after := rowCount(t, db.DB); after != before {
		t.Fatalf("a proposal wrote %d rows", after-before)
	}
}

func TestProposePageActionTakesTheSafeSteps(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	for _, c := range []struct{ args, action, target, text string }{
		{`{"action":"check","target":" GBP 200 \n total profit"}`, "check", "GBP 200 total profit", ""},
		{`{"action":"uncheck","target":"Newsletter"}`, "uncheck", "Newsletter", ""},
		{`{"action":"select","target":"Country","text":"Ireland"}`, "select", "Country", "Ireland"},
		{`{"action":"hover","target":"Menu"}`, "hover", "Menu", ""},
		{`{"action":"scroll","text":" Down "}`, "scroll", "", "down"},
		{`{"action":"press_key","text":"Tab"}`, "press_key", "", "Tab"},
	} {
		out, err := mt.Execute("propose_page_action", c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		_, p := proposalOf(t, out)
		if p["action"] != c.action || str(p["target"]) != c.target || str(p["text"]) != c.text {
			t.Errorf("%s -> %v", c.args, p)
		}
	}
}

func str(v interface{}) string { s, _ := v.(string); return s }
