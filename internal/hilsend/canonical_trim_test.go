package hilsend

import "testing"

// The gate reads a padded operation as the operation it pads (opOf trims):
// the built-in nodes refuse " post_message ", so this only ever holds a step
// that would fail, and no reader that trims can run a send the gate let
// through. A blank one made of spaces is the blank operation, which sends
// on Telegram.
func TestAPaddedOperationIsHeld(t *testing.T) {
	for _, c := range []struct{ nodeType, op string }{
		{"comm.slack", " post_message "},
		{"comm.telegram", "\tsend_message\n"},
		{"telegram", "  "},
		{"service.mastodon", " create_status"},
		{"gmail", "reply "},
	} {
		if !IsOutbound(c.nodeType, map[string]interface{}{"operation": c.op}) {
			t.Errorf("%s with operation %q is not held", c.nodeType, c.op)
		}
	}
	if IsOutbound("comm.slack", map[string]interface{}{"operation": " list_channels "}) {
		t.Error("a padded read is held as a send")
	}
}
