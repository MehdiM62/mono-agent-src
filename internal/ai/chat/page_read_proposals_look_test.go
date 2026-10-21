package chat

import (
	"strings"
	"testing"
)

func TestProposePageLookAsksAndSendsNothing(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	before := rowCount(t, db.DB)

	out, err := mt.Execute("propose_page_look", `{"why":"  The chart is\n drawn on a canvas "}`)
	if err != nil {
		t.Fatal(err)
	}
	res, p := proposalOf(t, out)
	if p["kind"] != "look" || p["why"] != "The chart is drawn on a canvas" {
		t.Fatalf("proposal = %v", p)
	}
	if res["added"] != false {
		t.Fatalf("the answer must say nothing was done: %s", out)
	}
	// The reason is optional.
	if _, err := mt.Execute("propose_page_look", `{}`); err != nil {
		t.Fatalf("a look needs no reason: %v", err)
	}
	if after := rowCount(t, db.DB); after != before {
		t.Fatalf("a proposal wrote %d rows", after-before)
	}
}

func TestProposePageLookBoundsTheReasonAndIsPageReadOnly(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	mt.SetPageReadOnly()
	out, err := mt.Execute("propose_page_look", `{"why":"`+strings.Repeat("é", 1000)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, p := proposalOf(t, out); len([]rune(p["why"].(string))) > coderWhyRunes {
		t.Fatalf("why not capped: %d runes", len([]rune(p["why"].(string))))
	}
	if _, err := mt.Execute("propose_page_look", `not json`); err == nil {
		t.Error("accepted unreadable arguments")
	}
	// Outside a page-read session the tool does not exist.
	full := NewMonoagentTools(db.DB, "/bin/monoagentcli", "default")
	if _, err := full.Execute("propose_page_look", `{}`); err == nil {
		t.Error("propose_page_look ran outside a page-read session")
	}
	found := false
	for _, n := range PageReadToolNames {
		found = found || n == "propose_page_look"
	}
	if !found {
		t.Error("propose_page_look is not in PageReadToolNames")
	}
}
