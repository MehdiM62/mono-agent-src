//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NodeConfigFromCurl shells out to `node curl -` with the command on stdin
// and no global flags, keeping only the CLI's "note:" lines.
func TestNodeConfigFromCurlShellsOut(t *testing.T) {
	dir := t.TempDir()
	log, in := filepath.Join(dir, "args.log"), filepath.Join(dir, "stdin")
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo "$*" >> '`+log+`'
cat > '`+in+`'
echo 'applied migration 001' >&2
echo '{"method":"POST","url":"https://api.example.com/items","headers":{"Accept":"application/json"}}'
echo 'note: option --compressed ignored' >&2
`))
	a := newTestApp(t)
	got := a.NodeConfigFromCurl("curl -X POST https://api.example.com/items --compressed")
	if got.Error != "" || got.Config["method"] != "POST" || got.Config["url"] != "https://api.example.com/items" {
		t.Fatalf("NodeConfigFromCurl = %+v", got)
	}
	if h, _ := got.Config["headers"].(map[string]interface{}); h["Accept"] != "application/json" {
		t.Fatalf("headers = %v", got.Config["headers"])
	}
	if len(got.Notes) != 1 || got.Notes[0] != "option --compressed ignored" {
		t.Fatalf("notes = %q", got.Notes)
	}
	if args := loggedArgs(t, log); strings.Join(args, "|") != "node curl -" {
		t.Fatalf("CLI args = %q", args)
	}
	if b, _ := os.ReadFile(in); string(b) != "curl -X POST https://api.example.com/items --compressed" {
		t.Fatalf("stdin = %q", b)
	}
}

// A parse failure comes back as the CLI's last stderr line.
func TestNodeConfigFromCurlError(t *testing.T) {
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, "echo 'invalid input: not a curl command' >&2; exit 4\n"))
	got := newTestApp(t).NodeConfigFromCurl("wget x")
	if got.Error != "invalid input: not a curl command" || got.Config != nil {
		t.Fatalf("NodeConfigFromCurl = %+v", got)
	}
}
