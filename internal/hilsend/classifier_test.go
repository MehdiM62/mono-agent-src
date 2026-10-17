package hilsend

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeClassifier sends for app.fake's "send" op, and its "reply" op whose
// text is an option of a collection param.
type fakeClassifier struct{}

func (fakeClassifier) Classify(nodeType string, config map[string]interface{}) (Classified, bool) {
	if nodeType != "app.fake" {
		return Classified{}, false
	}
	switch config["operation"] {
	case "send":
		return Classified{Action: ActionDM, Channel: "fake", To: []string{"to"}, Text: []string{"text"}, PerItem: []string{"to", "text"}}, true
	case "reply":
		return Classified{Action: ActionComment, Channel: "fake", To: []string{"ticket"}, Text: []string{"updateFields.publicReply"}}, true
	}
	return Classified{}, false
}

func (fakeClassifier) SendsSome(nodeType string) bool { return nodeType == "app.fake" }

// Node types the table doesn't know are sends when the installed classifier
// says so, judged on the operation the run resolved, or, ahead of the run,
// on one only the run resolves; without a classifier they send as before.
func TestClassifierExtendsTheTable(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	SetClassifier(nil)
	if _, handled, _ := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{"operation": "send"}); handled {
		t.Fatal("held with no classifier installed")
	}
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{Items: []workflow.Item{
		workflow.NewItem(map[string]interface{}{"who": "+15550100"}),
		workflow.NewItem(map[string]interface{}{"who": "+15550101"}),
	}}, map[string]interface{}{"operation": "send", "to": "{{ $json.who }}", "text": "Hi"})
	if !handled || err != nil || len(out[0].Items) != 2 {
		t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
	}
	for i, want := range []string{"+15550100", "+15550101"} {
		p, err := (Store{db, "p1"}).Get(ctxFor(db), out[0].Items[i].JSON["hil_id"].(string))
		if err != nil || p.Recipient.Name != want || p.Channel != "fake" || p.Account.Platform != "fake" || p.Action != ActionDM {
			t.Fatalf("item %d = %+v, %v", i, p, err)
		}
	}
	for cfg, want := range map[string]bool{"send": true, "list": false, "{{ $json.op }}": true} {
		if got := MaySend("app.fake", map[string]interface{}{"operation": cfg}); got != want {
			t.Errorf("MaySend(%q) = %v", cfg, got)
		}
	}
	if MaySend("app.other", map[string]interface{}{"operation": "{{ $json.op }}"}) {
		t.Error("a node type that never sends may send")
	}
}

// A send's text can be an option of a collection param (an API package's
// "updateFields.publicReply"): the card shows it, and the approved run gets
// the text the user approved there, with the collection's other options.
func TestASendsTextInACollection(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{
		"operation": "reply", "ticket": "T-1", "updateFields": map[string]interface{}{"publicReply": "Fixed it", "status": "solved"}})
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	st := Store{db, "p1"}
	id := out[0].Items[0].JSON["hil_id"].(string)
	if p, err := st.Get(ctxFor(db), id); err != nil || p.Text != "Fixed it" || p.Recipient.Name != "T-1" {
		t.Fatalf("card = %+v, %v", p, err)
	}
	f := &fakeRunner{}
	edited := "Fixed it, sorry for the wait"
	if _, err := Resolve(context.Background(), st, id, true, Edit{Text: &edited}, false, f.run); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("runs = %d", len(f.calls))
	}
	if uf, _ := f.calls[0].Config["updateFields"].(map[string]interface{}); uf["publicReply"] != edited || uf["status"] != "solved" {
		t.Errorf("the approved run's updateFields = %v", f.calls[0].Config["updateFields"])
	}
}

// A send whose credential is written into its config can't wait for
// approval, which keeps no credentials: it is refused, with how to fix it,
// not queued as a card that can only fail. One resolved from a saved
// connection waits as before.
func TestAnInlineCredentialIsRefusedNotQueued(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	inline := map[string]interface{}{"operation": "send", "to": "+15550100", "text": "Hi",
		"credential": map[string]interface{}{"api_key": "k"}}
	if _, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, inline); !handled || err == nil ||
		!strings.Contains(err.Error(), "connect save") || !strings.Contains(err.Error(), "credential_id") {
		t.Fatalf("inline: handled=%v err=%v", handled, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hil_pending`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queued %d item(s), %v", n, err)
	}
	saved := map[string]interface{}{"operation": "send", "to": "+15550100", "text": "Hi", "credential_id": "fake",
		"api_key": "k", "credential": map[string]interface{}{"api_key": "k"}}
	if out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, saved); !handled || err != nil || len(out[0].Items) != 1 {
		t.Fatalf("saved: handled=%v err=%v", handled, err)
	}
}
