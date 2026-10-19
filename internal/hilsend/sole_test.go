package hilsend

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// An always-allow rule for ada@ stands for a send to ada@ alone. A copy to
// anyone else, a second address or a reply-to elsewhere gets its card
// (security review of #871: the rule matched on to and ignored cc and bcc).
func TestAlwaysAllowStandsForTheSoleRecipientOnly(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	st := Store{db, "p1"}
	if _, err := Resolve(context.Background(), st, queueEmail(t, db), true, Edit{}, true, (&fakeRunner{}).run); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); handled || err != nil {
		t.Fatalf("the plain send to the allowed recipient was queued: handled=%v err=%v", handled, err)
	}
	same := emailConfig()
	same["reply_to"] = "Ada@Example.test"
	same["in_reply_to"] = "<abc@mail.example.test>"
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, same); handled {
		t.Fatal("a reply-to to the same address, or a thread id, queued the send")
	}

	const evil = "evil@attacker.test"
	for name, change := range map[string]func(map[string]interface{}){
		"cc":                  func(c map[string]interface{}) { c["cc"] = evil },
		"bcc":                 func(c map[string]interface{}) { c["bcc"] = evil },
		"cc as a list":        func(c map[string]interface{}) { c["cc"] = []interface{}{evil} },
		"BCC in another case": func(c map[string]interface{}) { c["BCC"] = evil },
		"bcc inside options":  func(c map[string]interface{}) { c["options"] = map[string]interface{}{"bcc": evil} },
		"Graph-style ccRecipients": func(c map[string]interface{}) {
			c["ccRecipients"] = []interface{}{map[string]interface{}{"emailAddress": map[string]interface{}{"address": evil}}}
		},
		"a second address in to":  func(c map[string]interface{}) { c["to"] = "ada@example.test, " + evil },
		"to as a list of two":     func(c map[string]interface{}) { c["to"] = []interface{}{"ada@example.test", evil} },
		"another recipient param": func(c map[string]interface{}) { c["recipient"] = evil },
		"a reply-to elsewhere":    func(c map[string]interface{}) { c["reply_to"] = evil },
		"a replyTo elsewhere":     func(c map[string]interface{}) { c["replyTo"] = []interface{}{evil} },
		"a recipients list":       func(c map[string]interface{}) { c["recipients"] = []interface{}{evil} },
		"a cc under a list of messages": func(c map[string]interface{}) {
			c["messages"] = []interface{}{map[string]interface{}{"cc": evil}}
		},
	} {
		cfg := emailConfig()
		change(cfg)
		if _, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, cfg); !handled || err != nil {
			t.Errorf("%s: the send went without a card (handled=%v err=%v)", name, handled, err)
		}
	}
}

func TestSoleRecipientReadsEveryShape(t *testing.T) {
	email := target{to: []string{"to"}}
	for _, c := range []struct {
		name   string
		config map[string]interface{}
		want   bool
	}{
		{"one address", map[string]interface{}{"to": "ada@x.test"}, true},
		{"one address and an empty cc", map[string]interface{}{"to": "ada@x.test", "cc": "", "bcc": []interface{}{}}, true},
		{"one address written twice", map[string]interface{}{"to": "ada@x.test", "recipient": "ADA@x.test"}, true},
		{"two addresses with a semicolon", map[string]interface{}{"to": "ada@x.test; bo@x.test"}, false},
		{"a cc", map[string]interface{}{"to": "ada@x.test", "Cc": "bo@x.test"}, false},
		{"a reply-to object", map[string]interface{}{"to": "ada@x.test", "replyTo": map[string]interface{}{"address": "bo@x.test"}}, false},
	} {
		if got := soleRecipient(email, c.config); got != c.want {
			t.Errorf("%s: soleRecipient = %v, want %v", c.name, got, c.want)
		}
	}
}
