package hilsend

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A send whose recipient is a label ("the invoice's client") names no
// destination: always allow is refused before anything runs, the item stays
// pending for a plain approve, and a rule keyed on the label lets no later
// send through. Its Communications row is our own account's, not a People
// row named after the label.
func TestALabelIsNoDestination(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	st := Store{db, "p1"}
	queue := func(cfg map[string]interface{}) (string, bool) {
		t.Helper()
		out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			return "", false
		}
		return out[0].Items[0].JSON["hil_id"].(string), true
	}
	id, _ := queue(map[string]interface{}{"operation": "remind", "text": "Pay up"})
	p, err := st.Get(ctxFor(db), id)
	if err != nil || p.Recipient.Name != "the invoice's client" || !p.Recipient.Label {
		t.Fatalf("card = %+v, %v", p.Recipient, err)
	}
	f := &fakeRunner{}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, true, f.run); !errors.Is(err, ErrAlwaysLabel) {
		t.Fatalf("always allow on a label: %v", err)
	}
	if p, _ := st.Get(ctxFor(db), id); p.State != StatePending || len(f.calls) != 0 {
		t.Fatalf("after the refused always allow: state %s, runs %d", p.State, len(f.calls))
	}
	if rules, _ := st.Allows(ctxFor(db)); len(rules) != 0 {
		t.Fatalf("rules = %+v", rules)
	}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, false, f.run); err != nil || len(f.calls) != 1 {
		t.Fatalf("plain approve: %v, runs %d", err, len(f.calls))
	}

	for _, label := range []string{"the invoice's client", "the client of invoice inv9"} {
		if err := st.Allow(ctxFor(db), Allow{Action: p.Action, Platform: p.Account.Platform, Account: p.Account.Handle, Recipient: label}); err != nil {
			t.Fatal(err)
		}
	}
	for _, cfg := range []map[string]interface{}{
		{"operation": "remind", "text": "Pay up"},
		{"operation": "remind", "invoiceId": "inv9", "text": "Pay up"},
	} {
		if _, held := queue(cfg); !held {
			t.Errorf("a rule keyed on a label let %v through", cfg)
		}
	}
	if err := st.Allow(ctxFor(db), Allow{Action: p.Action, Platform: p.Account.Platform, Account: p.Account.Handle, Recipient: "ada@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, held := queue(map[string]interface{}{"operation": "remind", "email": "ada@example.test", "text": "Pay up"}); held {
		t.Error("a rule for an address no longer lets that address through")
	}

	var named int
	if err := db.QueryRow(`SELECT COUNT(*) FROM people WHERE platform_username LIKE 'the %' OR full_name LIKE 'the %'`).Scan(&named); err != nil || named != 0 {
		t.Fatalf("People rows named after a label: %d, %v", named, err)
	}
	var own int
	if err := db.QueryRow(`SELECT COUNT(*) FROM person_messages WHERE hil_ref = ? AND json_extract(metadata, '$.own_account_item')`, id).Scan(&own); err != nil || own != 1 {
		t.Fatalf("own-account rows of the labelled send: %d, %v", own, err)
	}

	items := WorkflowItems("p1", "wf", []commsync.Node{{ID: "n1", Type: "app.fake",
		Config: map[string]interface{}{"operation": "remind", "invoiceId": "inv9", "text": "Pay up"}}},
		[]map[string]interface{}{{"x": 1}})
	if items[0] == nil || items[0].Recipient != (commsync.Recipient{}) {
		t.Fatalf("a workflow approval's recipient = %+v", items[0])
	}
}

// Each check stands on its own: Resolve's read before the edit leaves a
// label card as it was, and claim, on what it claims, refuses always allow
// for a label and leaves it pending, while a plain claim goes through.
func TestAlwaysAllowOnALabelIsRefusedTwice(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	st := Store{db, "p1"}
	newCard := func() string {
		t.Helper()
		out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{"operation": "remind", "text": "Pay up"})
		if err != nil || !handled {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
		return out[0].Items[0].JSON["hil_id"].(string)
	}

	id := newCard()
	before, _ := st.Get(ctxFor(db), id)
	edited := "Pay now"
	f := &fakeRunner{}
	if _, err := Resolve(context.Background(), st, id, true, Edit{Text: &edited}, true, f.run); !errors.Is(err, ErrAlwaysLabel) {
		t.Fatalf("always allow with an edit: %v", err)
	}
	if p, _ := st.Get(ctxFor(db), id); p.Text != before.Text || p.State != StatePending || len(f.calls) != 0 {
		t.Fatalf("after the refusal: text %q, state %s, runs %d", p.Text, p.State, len(f.calls))
	}

	id = newCard()
	if _, _, _, claimed, err := st.claim(ctxFor(db), id, true); claimed || !errors.Is(err, ErrAlwaysLabel) {
		t.Fatalf("claim with always: claimed=%v err=%v", claimed, err)
	}
	if p, _ := st.Get(ctxFor(db), id); p.State != StatePending {
		t.Fatalf("after the refused claim: %s", p.State)
	}
	if _, _, _, claimed, err := st.claim(ctxFor(db), id, false); !claimed || err != nil {
		t.Fatalf("plain claim: claimed=%v err=%v", claimed, err)
	}
}
