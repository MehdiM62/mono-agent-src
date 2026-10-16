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

// An item queued before the decision was recorded is the operator's,
// whatever it sends: one in front of nothing that sends, with no
// Communications row, too. A new item records the decision both ways.
func TestHILItemQueuedBeforeTheDecisionIsTheOperators(t *testing.T) {
	r := newCommsRig(t, "transform.set", map[string]interface{}{"field": "x"}, nil, map[string]interface{}{})
	if err := r.run(""); !errors.Is(err, workflow.ErrExecutionPaused) {
		t.Fatalf("first run = %v", err)
	}
	if v, ok := r.nodeConfig()[commsync.OutboundKey].(bool); !ok || v {
		t.Fatalf("a new item in front of nothing that sends: outbound = %v (recorded %v)", v, ok)
	}
	id := r.hilID()
	if err := operator.ItemRefusal(r.ctx, r.db, "default", id); err != nil {
		t.Fatalf("a new item that sends nothing was refused: %v", err)
	}
	if _, err := r.db.Exec(`UPDATE hil_pending SET node_config = '{"readonly_fields":null,"editable_fields":null}' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	var n int
	r.db.QueryRow(`SELECT COUNT(*) FROM person_messages WHERE hil_ref = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("setup: %d Communications rows", n)
	}
	var only *operator.ItemError
	if err := operator.ItemRefusal(r.ctx, r.db, "default", id); !errors.As(err, &only) || !only.Message {
		t.Fatalf("ItemRefusal of an old row = %v", err)
	}
	for nc, want := range map[string]bool{`{"outbound":false}`: false, `{"outbound":true}`: true, `{}`: true, `not json`: true, `{"outbound":"no"}`: true} {
		if got := commsync.OutboundHIL(nc); got != want {
			t.Errorf("OutboundHIL(%s) = %v, want %v", nc, got, want)
		}
	}
}
