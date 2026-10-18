package workflowvalidate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// planted is a template wrapping a workflow whose Trello node sends a key
// its op doesn't declare and whose Freshservice node gives a merged
// collection as an expression; clean uses declared keys only.
const (
	planted = `{"name": "t", "workflow": {"nodes": [
	  {"id": "t1", "type": "trigger.manual", "config": {}},
	  {"id": "c1", "name": "Card", "type": "app.trello", "config": {"resource": "card", "operation": "update", "id": "x",
	    "updateFields": {"name": "N", "zz_extra": "x"}}},
	  {"id": "f1", "type": "app.freshservice", "config": {"resource": "ticket", "operation": "create", "email": "a@example.test",
	    "subject": "S", "description": "D", "additionalFields": "{{ $json.extra }}"}}]}}`
	clean = `{"nodes": [
	  {"id": "t1", "type": "trigger.manual", "config": {}},
	  {"id": "c1", "type": "app.trello", "config": {"resource": "card", "operation": "update", "id": "x", "updateFields": {"name": "N"}}},
	  {"id": "s1", "type": "core.set", "config": {"zz_extra": 1}}]}`
)

func checkKeys(t *testing.T, asJSON bool, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := NewCheckKeysCommand(func() bool { return asJSON })
	cmd.SilenceUsage, cmd.SilenceErrors = true, true // as the root has them
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	code := clicore.ExitCodeFor(cmd.Execute())
	return out.String(), code
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A planted bad file is reported node by node, with its op and keys, and
// exits 1; a clean one exits 0.
func TestCheckKeysReportsAPlantedFile(t *testing.T) {
	dir := t.TempDir()
	bad, good := writeFile(t, dir, "bad.json", planted), writeFile(t, dir, "good.json", clean)
	out, code := checkKeys(t, true, "", bad, good)
	var rep CheckKeysJSON
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != 1 {
		t.Fatalf("exit %d, %v:\n%s", code, err, out)
	}
	want := []KeyFinding{
		{File: bad, NodeID: "c1", Node: "Card", Type: "app.trello", Op: "trello card/update", Key: "zz_extra"},
		{File: bad, NodeID: "f1", Type: "app.freshservice", Op: "freshservice ticket/create", Key: "additionalFields (an expression)"},
	}
	if rep.Clean || rep.Files != 2 || rep.AppNodes != 3 || !reflect.DeepEqual(rep.Findings, want) || rep.Errors != nil {
		t.Errorf("report %+v, want findings %+v", rep, want)
	}

	out, code = checkKeys(t, false, "", good)
	if code != 0 || out != "1 file(s), 1 app node(s): 0 undeclared key(s)\n" {
		t.Errorf("clean: exit %d\n%s", code, out)
	}
	out, code = checkKeys(t, false, "", bad)
	if code != 1 || !strings.Contains(out, bad+`: node "c1" (Card), app.trello trello card/update: undeclared zz_extra`) {
		t.Errorf("human: exit %d\n%s", code, out)
	}
}

// stdin is read with - or no file; a file that can't be read or isn't JSON
// exits 3, and the others are still checked.
func TestCheckKeysReadsStdinAndReportsBadFiles(t *testing.T) {
	for _, args := range [][]string{nil, {"-"}} {
		out, code := checkKeys(t, true, planted, args...)
		if code != 1 || !strings.Contains(out, `"file":"-","node_id":"c1"`) {
			t.Errorf("stdin %q: exit %d\n%s", args, code, out)
		}
	}
	dir := t.TempDir()
	broken := writeFile(t, dir, "broken.json", "{")
	out, code := checkKeys(t, true, "", broken, filepath.Join(dir, "missing.json"), writeFile(t, dir, "bad.json", planted))
	var rep CheckKeysJSON
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != 3 || rep.Files != 1 || len(rep.Findings) != 2 || len(rep.Errors) != 2 || rep.Clean {
		t.Errorf("exit %d, %v: %+v", code, err, rep)
	}
}
