package monomind

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExecAttach: each Attach file goes to agent exec as its own --attach,
// in order; without any, no --attach at all.
func TestExecAttach(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake monomind is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "monomind")
	record := filepath.Join(dir, "argv")
	os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+record+"\n"+
		`echo '{"v":1,"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","text":"ok"}'`+"\n"+
		`echo '{"v":1,"type":"done","exit_code":0}'`+"\n"), 0o755)
	argv := func(attach []string) string {
		t.Helper()
		if _, err := Exec(context.Background(), ExecOptions{Bin: bin, Runtime: "claude", Prompt: "p", Attach: attach}, nil); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(record)
		return string(b)
	}
	got := argv([]string{"/w/.monoagent/attachments/t1/a b.png", "/w/.monoagent/attachments/t1/notes.txt"})
	if !strings.Contains(got, "--attach\n/w/.monoagent/attachments/t1/a b.png\n--attach\n/w/.monoagent/attachments/t1/notes.txt\n") {
		t.Errorf("argv lacks one --attach per file, in order:\n%s", got)
	}
	if got := argv(nil); strings.Contains(got, "--attach") {
		t.Errorf("argv without attachments has --attach:\n%s", got)
	}
}
