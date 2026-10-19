package hil

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// `hil allow add` checks a rule before it stores it, so the app's form and
// every other caller get the same answer (security review of #871: an
// unknown action, *, %, a 5,000-character recipient and "the ticket's
// requester" were all stored).
func TestHILAllowAddRefusesAWrongRule(t *testing.T) {
	cfg := &testConfig{DBPath: newHILTestDB(t)}
	d := testDeps(cfg)
	d.RequireOperator = func(string) error { return nil }
	run := func(args ...string) (string, error) {
		var buf bytes.Buffer
		cmd := d.newHILCmd()
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(append([]string{"allow"}, args...))
		err := cmd.Execute()
		return buf.String(), err
	}
	for _, c := range []struct {
		name, action, recipient, want string
	}{
		{"an unknown action", "bogus", "ada@example.test", "--action must be one of email, dm, comment, post, connect, publish"},
		{"no action", "", "ada@example.test", "--action must be one of"},
		{"no recipient", "email", "  ", "--recipient is required"},
		{"a wildcard", "email", "*", "can't hold * or %"},
		{"a SQL wildcard", "email", "%@example.test", "can't hold * or %"},
		{"a recipient over 320 bytes", "email", strings.Repeat("a", 5000) + "@example.test", "a recipient is at most 320"},
		{"a label", "email", "the ticket's requester", "reads like a description, not an address"},
		{"a label behind a zero-width space", "dm", "\u200bThe customer", "reads like a description"},
	} {
		_, err := run("add", "--action", c.action, "--recipient", c.recipient)
		if exitCodeFor(err) != 3 || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v (exit %d), want exit 3 with %q", c.name, err, exitCodeFor(err), c.want)
		}
	}
	out, err := run("list")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("a refused rule was stored: %s %v", out, err)
	}

	if _, err := run("add", "--action", "email", "--recipient", "ada@example.test", "--platform", "smtp", "--account", "me@example.test"); err != nil {
		t.Fatalf("a good rule: %v", err)
	}
	out, _ = run("list")
	var rules []map[string]any
	if err := json.Unmarshal([]byte(out), &rules); err != nil || len(rules) != 1 || rules[0]["recipient"] != "ada@example.test" {
		t.Fatalf("rules = %s (%v)", out, err)
	}
}
