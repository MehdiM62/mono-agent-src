package hilsend

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeClassifier sends for app.fake's "send" op only.
type fakeClassifier struct{}

func (fakeClassifier) Classify(nodeType string, config map[string]interface{}) (Classified, bool) {
	if nodeType != "app.fake" || config["operation"] != "send" {
		return Classified{}, false
	}
	return Classified{Action: ActionDM, Channel: "fake", To: []string{"to"}, Text: []string{"text"}, PerItem: []string{"to", "text"}}, true
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
