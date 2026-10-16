package control

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

func (r *commsRig) nodeConfig() map[string]interface{} {
	r.t.Helper()
	var raw string
	if err := r.db.QueryRow(`SELECT node_config FROM hil_pending WHERE execution_id = 'ex1'`).Scan(&raw); err != nil {
		r.t.Fatal(err)
	}
	var nc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &nc); err != nil {
		r.t.Fatal(err)
	}
	return nc
}

// An item whose approval lets a message to a person go out (an email, a DM
// after the node) is marked so, which makes it the operator's to decide; an
// item in front of a node that sends nothing is not.
func TestHILMarksItemsThatLetAMessageGoOut(t *testing.T) {
	for _, tc := range []struct {
		name, typ string
		config    map[string]interface{}
		trigger   map[string]interface{}
		outbound  bool
	}{
		{"email", "comm.email_send", map[string]interface{}{"to": "ada@example.test", "subject": "Hi", "body": "{{ $json.body }}"},
			map[string]interface{}{"body": "Hello Ada"}, true},
		{"dm", "linkedin.send_dms", map[string]interface{}{"messageText": "Hello"},
			map[string]interface{}{"name": "Ada", "profileUrl": "https://www.linkedin.com/in/ada/", "messageText": "Hello"}, true},
		{"no send", "transform.set", map[string]interface{}{"field": "x"}, map[string]interface{}{"x": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCommsRig(t, tc.typ, tc.config, nil, tc.trigger)
			if err := r.run(""); !errors.Is(err, workflow.ErrExecutionPaused) {
				t.Fatalf("first run = %v", err)
			}
			if got, _ := r.nodeConfig()[commsync.OutboundKey].(bool); got != tc.outbound {
				t.Fatalf("outbound = %v, want %v (node_config %v)", got, tc.outbound, r.nodeConfig())
			}
			err := operator.ItemRefusal(r.ctx, r.db, "default", r.hilID())
			var only *operator.ItemError
			if refused := errors.As(err, &only) && only.Message; refused != tc.outbound {
				t.Errorf("ItemRefusal = %v, want refused %v", err, tc.outbound)
			}
		})
	}
}

// An item queued before the mark existed is still the operator's when it has
// a Communications row: only an outbound workflow item gets one.
func TestHILOutboundItemQueuedBeforeTheMark(t *testing.T) {
	r := newCommsRig(t, "comm.email_send", map[string]interface{}{"to": "ada@example.test", "subject": "Hi", "body": "Hello"}, nil,
		map[string]interface{}{})
	if err := r.run(""); !errors.Is(err, workflow.ErrExecutionPaused) {
		t.Fatalf("first run = %v", err)
	}
	id := r.hilID()
	if _, err := r.db.Exec(`UPDATE hil_pending SET node_config = '{"readonly_fields":null,"editable_fields":null}' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if out, err := commsync.OutboundHIL(r.ctx, r.db, id, `{}`); err != nil || !out {
		t.Fatalf("OutboundHIL of an old row = %v, %v", out, err)
	}
	var only *operator.ItemError
	if err := operator.ItemRefusal(r.ctx, r.db, "default", id); !errors.As(err, &only) || !only.Message {
		t.Fatalf("ItemRefusal of an old row = %v", err)
	}
}
