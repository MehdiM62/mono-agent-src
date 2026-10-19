package checkkeys

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Fixtures the entry points share (workflow check-keys, scripts/checkkeys):
// planted.json is a template wrapping a workflow whose Trello node sends a
// key its op doesn't declare and whose Freshservice node gives a merged
// collection as an expression; clean.json uses declared keys only.
const (
	planted = "testdata/planted.json"
	clean   = "testdata/clean.json"
)

// A planted file's nodes are found inside the template, each undeclared key
// with its node, op and key; a clean file and a non-app node are clean.
func TestCheckFindsAPlantedFile(t *testing.T) {
	rep, err := Check(nil, []string{planted, clean})
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{File: planted, NodeID: "c1", Node: "Card", Type: "app.trello", Op: "trello card/update", Key: "zz_extra"},
		{File: planted, NodeID: "f1", Type: "app.freshservice", Op: "freshservice ticket/create", Key: "additionalFields (an expression)"},
	}
	if rep.Clean || rep.Files != 2 || rep.AppNodes != 3 || !reflect.DeepEqual(rep.Findings, want) || rep.Errors != nil ||
		rep.ExitCode() != ExitUndeclared || rep.Reason() != "2 undeclared key(s) in 2 file(s)" {
		t.Errorf("report %+v (exit %d, %q), want findings %+v", rep, rep.ExitCode(), rep.Reason(), want)
	}
	var out bytes.Buffer
	rep.Write(&out, false)
	if !strings.HasPrefix(out.String(), planted+`: node "c1" (Card), app.trello trello card/update: undeclared zz_extra`+"\n") ||
		!strings.HasSuffix(out.String(), "2 file(s), 3 app node(s): 2 undeclared key(s)\n") {
		t.Errorf("text:\n%s", out.String())
	}

	rep, _ = Check(nil, []string{clean})
	if !rep.Clean || rep.ExitCode() != ExitClean || rep.Reason() != "" || rep.AppNodes != 1 {
		t.Errorf("clean: %+v", rep)
	}
}

// stdin is read with - or no file. A file that can't be read or isn't JSON
// is reported and exits 3, and the others are still checked.
func TestCheckReadsStdinAndReportsBadFiles(t *testing.T) {
	b, err := os.ReadFile(planted)
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{nil, {"-"}} {
		if rep, _ := Check(bytes.NewReader(b), names); len(rep.Findings) != 2 || rep.Findings[0].File != "-" {
			t.Errorf("stdin %q: %+v", names, rep)
		}
	}
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, _ := Check(nil, []string{broken, filepath.Join(dir, "missing.json"), planted})
	if rep.Clean || rep.Files != 1 || len(rep.Findings) != 2 || len(rep.Errors) != 2 || rep.ExitCode() != ExitBadInput ||
		!strings.HasPrefix(rep.Reason(), broken+": ") {
		t.Errorf("%+v (exit %d)", rep, rep.ExitCode())
	}
}

// A file that isn't JSON is reported without any of its content (the
// decoder's message quotes the character it stopped at), at most the byte
// it stopped at.
func TestCheckDoesNotEchoAFilesContent(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("sk-live-1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, _ := Check(nil, []string{secret})
	if len(rep.Errors) != 1 || rep.Errors[0] != secret+": not valid JSON (at byte 1)" {
		t.Errorf("errors %q", rep.Errors)
	}
}

// zeros is an endless input.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// A file or stdin over MaxInput is reported, not read into memory, and the
// other files are still checked.
func TestCheckCapsTheInput(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.json")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxInput + 1); err != nil { // sparse
		t.Fatal(err)
	}
	f.Close()
	rep, _ := Check(nil, []string{big, planted})
	if len(rep.Errors) != 1 || rep.Errors[0] != big+": larger than 16 MB" || len(rep.Findings) != 2 || rep.ExitCode() != ExitBadInput {
		t.Errorf("file: %+v", rep)
	}
	rep, _ = Check(zeros{}, []string{"-"})
	if len(rep.Errors) != 1 || rep.Errors[0] != "-: larger than 16 MB" {
		t.Errorf("stdin: %+v", rep.Errors)
	}
}
