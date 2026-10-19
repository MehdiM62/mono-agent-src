package noderegistry

import (
	"context"
	"errors"
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
