package chat

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/coderhost"
)

func TestParkAndRecordActOnTheCallersOwnSessionWithTheTokenOnly(t *testing.T) {
	m := newMasterTurn(t)
	cs := openCoder(t, m.dbPath)

	out, code := runSessionsCLI(t, m.cfg, "park", "--class", "risky_action", "--question", "delete the build dir?", "--option", "yes", "--option", "no")
	if code != 0 || !strings.Contains(out, "delete the build dir?") {
		t.Fatalf("park: %d %s", code, out)
	}
	if cs.OpenParked("default", m.master) != 1 {
		t.Fatal("the question was not parked on the caller's session")
	}
	// a hard class is never reversible; the model-decided gap is not parked
	for _, args := range [][]string{
		{"park", "--class", "risky_action", "--question", "q", "--reversible"},
		{"park", "--class", "nonsense", "--question", "q"},
		{"park", "--class", "ambiguity", "--question", "tabs?", "--reversible"},
		{"record", "--class", "risky_action", "--choice", "do it"},
		{"record", "--class", "missing_credential", "--choice", "use a guess", "--reversible"},
		{"record", "--class", "ambiguity", "--choice", "c"}, // irreversible: the policy leaves it to a person
	} {
		if out, code := runSessionsCLI(t, m.cfg, args...); code == 0 {
			t.Errorf("%v was allowed: %s", args, out)
		}
	}
	out, code = runSessionsCLI(t, m.cfg, "park", "--class", "ambiguity", "--question", "tabs?", "--reversible")
	if !strings.Contains(out, "coder sessions record") {
		t.Fatalf("the refusal does not tell the model to record: %s", out)
	}
	if out, code := runSessionsCLI(t, m.cfg, "record", "--class", "ambiguity", "--question", "tabs?", "--choice", "tabs", "--alternative", "spaces", "--reason", "repo style", "--reversible"); code != 0 {
		t.Fatalf("record: %d %s", code, out)
	}
	rows, _ := cs.ListDecisions("default", m.master, coderhost.LedgerFilter{})
	if len(rows) != 1 || rows[0].MadeBy != "model" || rows[0].Choice != "tabs" {
		t.Fatalf("ledger = %+v", rows)
	}
	if len(pendingCommands(t, m.dbPath)) != 0 {
		t.Fatal("park or record queued a command")
	}

	// no token: the operator has no session of its own; a forged token: a refusal
	t.Setenv(coderhost.SessionTokenEnv, "")
	if _, code := runSessionsCLI(t, m.cfg, "park", "--class", "risky_action", "--question", "q"); code != 3 {
		t.Fatalf("park without a token: exit %d", code)
	}
	if _, code := runSessionsCLI(t, m.cfg, "record", "--class", "ambiguity", "--choice", "c", "--reversible"); code != 3 {
		t.Fatalf("record without a token: exit %d", code)
	}
	t.Setenv(coderhost.SessionTokenEnv, "mst-forged")
	if _, code := runSessionsCLI(t, m.cfg, "park", "--class", "risky_action", "--question", "q"); code == 0 {
		t.Fatal("a forged token parked")
	}
	if cs.OpenParked("default", m.master) != 1 {
		t.Fatal("a refused park left a row")
	}
}
