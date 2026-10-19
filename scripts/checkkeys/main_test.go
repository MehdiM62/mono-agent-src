package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/checkkeys"
	"github.com/monoes/mono-agent/internal/testhome"
)

// helperEnv makes the test binary the tool itself (TestMain), so a test can
// run it as its own process, package initialisation included.
const helperEnv = "CHECKKEYS_TEST_AS_TOOL"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	testhome.Main(m)
}

// The fixtures internal/checkkeys tests the check on.
const (
	planted = "../../internal/checkkeys/testdata/planted.json"
	clean   = "../../internal/checkkeys/testdata/clean.json"
)

// runBare runs the tool as its own process with HOME an empty directory and
// nothing else of the environment but PATH, and fails if it wrote anything
// there.
func runBare(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = []string{helperEnv + "=1", "HOME=" + home, "PATH=" + os.Getenv("PATH")}
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("%q wrote %s to HOME", args, e.Name())
	}
	return out.String(), errb.String(), code
}

// Signed out and with an empty HOME, the tool reports a planted file with
// exit 1, as the JSON document alone on stdout with --json, a clean one with
// exit 0, and stdin with -; it writes nothing under HOME.
func TestCheckKeysToolRunsBare(t *testing.T) {
	stdout, stderr, code := runBare(t, "", planted)
	if code != 1 || stderr != "" || !strings.Contains(stdout, planted+`: node "c1" (Card), app.trello trello card/update: undeclared zz_extra`) {
		t.Errorf("planted: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	stdout, stderr, code = runBare(t, "", "--json", planted, clean)
	var rep checkkeys.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil || code != 1 || len(rep.Findings) != 2 || rep.Files != 2 ||
		stderr != "2 undeclared key(s) in 2 file(s)\n" {
		t.Errorf("--json: exit %d, %v, stdout %q, stderr %q", code, err, stdout, stderr)
	}
	if stdout, stderr, code := runBare(t, "", clean); code != 0 || stderr != "" || stdout != "1 file(s), 1 app node(s): 0 undeclared key(s)\n" {
		t.Errorf("clean: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	b, err := os.ReadFile(planted)
	if err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runBare(t, string(b), "-"); code != 1 || !strings.Contains(stdout, `-: node "c1"`) {
		t.Errorf("stdin: exit %d, stdout %q", code, stdout)
	}
	if _, stderr, code := runBare(t, "", "no/such.json"); code != 3 || !strings.Contains(stderr, "no/such.json") {
		t.Errorf("missing file: exit %d, stderr %q", code, stderr)
	}
}

// A catalog that doesn't load checks nothing: exit 3, never the 1 of a
// finding.
func TestCheckKeysToolWithoutACatalogExits3(t *testing.T) {
	prev := checkKeys
	checkKeys = func(io.Reader, []string) (checkkeys.Report, error) {
		return checkkeys.Report{}, errors.New("load the API catalog: broken")
	}
	t.Cleanup(func() { checkKeys = prev })
	var out, errb bytes.Buffer
	if code := run([]string{clean}, strings.NewReader(""), &out, &errb); code != 3 || out.Len() != 0 || errb.String() != "load the API catalog: broken\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errb.String())
	}
}
