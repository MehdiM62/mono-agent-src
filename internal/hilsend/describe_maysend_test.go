package hilsend

import "testing"

// MaySend judges a node before its run: an operation only the run resolves
// counts as sending for a node type that sends for some operations.
func TestMaySend(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		op   interface{}
		want bool
	}{
		{"comm.slack", "post_message", true},
		{"comm.slack", "list_channels", false},
		{"comm.slack", "{{ $json.op }}", true},
		{"comm.slack", "={{$json.op}}", true},
		{"service.gmail", " {{ $json.operation }} ", true},
		{"service.gmail", "list_messages", false},
		{"comm.email_send", nil, true},
		{"transform.set", "{{ $json.op }}", false},
		{"linkedin.send_dms", nil, true},
	} {
		cfg := map[string]interface{}{}
		if tc.op != nil {
			cfg["operation"] = tc.op
		}
		if got := MaySend(tc.typ, cfg); got != tc.want {
			t.Errorf("MaySend(%s, %v) = %v, want %v", tc.typ, tc.op, got, tc.want)
		}
	}
	// What a run does is still judged on its resolved operation.
	if IsOutbound("comm.slack", map[string]interface{}{"operation": "{{ $json.op }}"}) {
		t.Error("IsOutbound now guesses at an unresolved operation")
	}
}
