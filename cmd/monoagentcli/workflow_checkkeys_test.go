package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/checkkeys"
)

// `workflow check-keys` is a gated command like any other (D6: nothing
// before a sign-in; scripts/checkkeys is the check for no sign-in). Signed
// in, it needs no --profile and reports a planted file with exit 1, the
// report the one document on stdout with --json, and a clean one with 0.
func TestWorkflowCheckKeysThroughTheCLI(t *testing.T) {
	const planted, clean = "../../internal/checkkeys/testdata/planted.json", "../../internal/checkkeys/testdata/clean.json"
	accounttest.Install(t, accounttest.SignedIn)
	freshHome(t)
	code, stdout, stderr := runMain(t, "--json", "workflow", "check-keys", planted, clean)
	var rep checkkeys.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil || code != 1 || len(rep.Findings) != 2 ||
		rep.Findings[0].Key != "zz_extra" || stderr != "2 undeclared key(s) in 2 file(s)\n" {
		t.Errorf("--json planted: exit %d, %v, stdout %q, stderr %q", code, err, stdout, stderr)
	}
	if code, stdout, stderr := runMain(t, "workflow", "check-keys", planted); code != 1 || stderr != "" || !strings.Contains(stdout, "undeclared zz_extra") {
		t.Errorf("planted: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, stdout, _ := runMain(t, "workflow", "check-keys", clean); code != 0 || stdout != "1 file(s), 1 app node(s): 0 undeclared key(s)\n" {
		t.Errorf("clean: exit %d, stdout %q", code, stdout)
	}

	accounttest.Install(t, accounttest.LockedRefused)
	if code, _, _ := runMain(t, "workflow", "check-keys", clean); code != 4 {
		t.Errorf("signed out: exit %d, want the gate's 4", code)
	}
}
