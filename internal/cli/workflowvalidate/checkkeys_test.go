package workflowvalidate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/checkkeys"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// The fixtures internal/checkkeys tests the check on.
const (
	planted = "../../checkkeys/testdata/planted.json"
	clean   = "../../checkkeys/testdata/clean.json"
)

func runCheckKeys(t *testing.T, asJSON bool, args ...string) (string, error) {
	t.Helper()
	cmd := NewCheckKeysCommand(func() bool { return asJSON })
	cmd.SilenceUsage, cmd.SilenceErrors = true, true // as the root has them
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// `workflow check-keys` reports a planted file with exit 1, as the JSON
// document alone with --json, and a clean one with exit 0; a file it can't
// read exits 3.
func TestCheckKeysCommandExitCodes(t *testing.T) {
	out, err := runCheckKeys(t, true, planted, clean)
	var rep checkkeys.Report
	if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil || clicore.ExitCodeFor(err) != 1 || len(rep.Findings) != 2 ||
		rep.Findings[0].Key != "zz_extra" || !errorsAs[clicore.Reported](err) {
		t.Errorf("--json planted: %v, %v\n%s", err, jerr, out)
	}
	out, err = runCheckKeys(t, false, planted)
	if clicore.ExitCodeFor(err) != 1 || !errorsAs[clicore.Shown](err) || !strings.Contains(out, "undeclared zz_extra") {
		t.Errorf("planted: %v\n%s", err, out)
	}
	if out, err := runCheckKeys(t, false, clean); err != nil || out != "1 file(s), 1 app node(s): 0 undeclared key(s)\n" {
		t.Errorf("clean: %v\n%s", err, out)
	}
	if _, err := runCheckKeys(t, false, "no/such.json"); clicore.ExitCodeFor(err) != 3 || !strings.Contains(err.Error(), "no/such.json") {
		t.Errorf("missing: %v", err)
	}
}

// A catalog that doesn't load checks nothing: exit 3, never the 1 of a
// finding.
func TestCheckKeysCommandWithoutACatalogExits3(t *testing.T) {
	prev := checkKeys
	checkKeys = func(io.Reader, []string) (checkkeys.Report, error) {
		return checkkeys.Report{}, errors.New("load the API catalog: broken")
	}
	t.Cleanup(func() { checkKeys = prev })
	if out, err := runCheckKeys(t, false, clean); clicore.ExitCodeFor(err) != 3 || out != "" || err.Error() != "load the API catalog: broken" {
		t.Errorf("%v (exit %d), stdout %q", err, clicore.ExitCodeFor(err), out)
	}
}

func errorsAs[T error](err error) bool {
	_, ok := err.(T)
	return ok
}
