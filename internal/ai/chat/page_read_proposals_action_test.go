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
