package noderegistry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A held Zulip stream update or Salesforce flow lists what it changes or
// passes on the card, read-only: a privacy change reads as one, a cleared
// description as "", a flow's variables as they go out, and a typed text
// can't become the stream's description.
func TestASettingsOrFlowCardListsWhatItSends(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	db := sendsDB(t)
	st := hilsend.Store{DB: db, Profile: "default"}
	type cfg = map[string]interface{}
	for _, c := range []struct {
		nodeType string
		config   cfg
		text     string
	}{
		{"app.zulip", cfg{"resource": "stream", "operation": "update", "streamId": "7", "additionalFields": cfg{"isPrivate": true}},
			"zulip stream/update with:\nadditionalFields.isPrivate: true"},
		{"app.zulip", cfg{"resource": "stream", "operation": "update", "streamId": "7", "additionalFields": cfg{"description": ""}},
			"zulip stream/update with:\nadditionalFields.description: \"\""},
		{"app.zulip", cfg{"resource": "stream", "operation": "update", "streamId": "7", "additionalFields": cfg{"description": "Launch", "streamPostPolicy": 2}},
			"zulip stream/update with:\nadditionalFields.description: \"Launch\"\nadditionalFields.streamPostPolicy: 2"},
		// Raw JSON lists each key: padding hides no change behind it.
		{"app.zulip", cfg{"resource": "stream", "operation": "update", "streamId": "7", "jsonParameters": true,
			"additionalFieldsJson": `{"description":"` + strings.Repeat("A", 600) + `","is_private":true}`},
			"zulip stream/update with:\nadditionalFieldsJson:\n  \"description\": \"" + strings.Repeat("A", 199) + "…\n  \"is_private\": true"},
		// A flow sends its JSON or its variables fields, by its switch: the
		// other is marked.
		{"service.salesforce", cfg{"resource": "flow", "operation": "invoke", "apiName": "Notify", "jsonParameters": true,
			"variablesJson": `{"body":"Hi Ada"}`, "variablesUi": cfg{"variablesValues": []interface{}{cfg{"name": "body", "value": "Hello"}}}},
			"salesforce flow/invoke with:\nvariablesUi.variablesValues (not sent): [{\"name\":\"body\",\"value\":\"Hello\"}]\nvariablesJson:\n  \"body\": \"Hi Ada\""},
		{"service.salesforce", cfg{"resource": "flow", "operation": "invoke", "apiName": "Notify",
			"variablesUi": cfg{"variablesValues": []interface{}{cfg{"name": "body", "value": "Hi Ada"}}}},
			"salesforce flow/invoke with:\nvariablesUi.variablesValues: [{\"name\":\"body\",\"value\":\"Hi Ada\"}]"},
	} {
		out, handled, err := hilsend.Guard(sendsCtx(db), c.nodeType, workflow.NodeInput{}, c.config)
		if err != nil || !handled {
			t.Fatalf("%s %v: handled=%v err=%v", c.nodeType, c.config, handled, err)
		}
		id := out[0].Items[0].JSON["hil_id"].(string)
		p, err := st.Get(context.Background(), id)
		if err != nil || p.Text != c.text || !p.TextReadOnly {
			t.Errorf("%s %v: text %q, read-only %v, %v; want %q", c.nodeType, c.config, p.Text, p.TextReadOnly, err, c.text)
		}
		typed := "CHANGED"
		if _, err := st.Edit(context.Background(), id, &typed, nil); !errors.Is(err, hilsend.ErrNoText) {
			t.Errorf("%s %v: an edit of the text: %v", c.nodeType, c.config, err)
		}
	}
}

// A Zulip stream update's recipient is a label ("stream 7"): no
// always-allow rule stands for it, so a rule a post to the stream left
// (recipient "7") doesn't free a privacy change, and approving its card
// with always-allow is refused, the card still pending.
func TestNoRuleFreesAZulipStreamChange(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	db := sendsDB(t)
	st := hilsend.Store{DB: db, Profile: "default"}
	private := map[string]interface{}{"resource": "stream", "operation": "update", "streamId": "7", "additionalFields": map[string]interface{}{"isPrivate": true}}
	out, handled, err := hilsend.Guard(sendsCtx(db), "app.zulip", workflow.NodeInput{}, private)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)
	p, err := st.Get(context.Background(), id)
	if err != nil || p.Recipient.Name != "stream 7" || !p.Recipient.Label {
		t.Errorf("recipient %+v, %v", p.Recipient, err)
	}
	if err := st.Allow(context.Background(), hilsend.Allow{Action: p.Action, Platform: p.Account.Platform, Account: p.Account.Handle, Recipient: "7"}); err != nil {
		t.Fatal(err)
	}
	for name, config := range map[string]map[string]interface{}{"a privacy change": private,
		"raw JSON": {"resource": "stream", "operation": "update", "streamId": "7", "jsonParameters": true, "additionalFieldsJson": `{"is_private":true}`}} {
		if _, handled, err := hilsend.Guard(sendsCtx(db), "app.zulip", workflow.NodeInput{}, config); err != nil || !handled {
			t.Errorf("%s on stream 7 with a rule for 7: handled=%v err=%v, want it held", name, handled, err)
		}
	}
	run := func(context.Context, hilsend.Run) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	if _, err := hilsend.Resolve(context.Background(), st, id, true, hilsend.Edit{}, true, run); !errors.Is(err, hilsend.ErrAlwaysLabel) {
		t.Errorf("approve with always-allow: %v", err)
	}
	if p, _ := st.Get(context.Background(), id); p.State != hilsend.StatePending {
		t.Errorf("after the refused always-allow the card is %s", p.State)
	}
}
