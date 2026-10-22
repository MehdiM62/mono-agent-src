//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func aitaskApp(t *testing.T, script string) (*App, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo "$*" >> '`+log+"'\n"+script))
	a := newTestApp(t)
	a.ctx = context.Background()
	a.selectForTest("work")
	return a, log
}

const taskJSON = `{"taskId":"t1","kind":"generic","conversationId":"c1","status":"running","goal":"g","createdAt":"c","updatedAt":"u","applicationId":"app-1"}`

func TestSubmitAITaskBuildsArgsAndReturnsTask(t *testing.T) {
	a, log := aitaskApp(t, "echo '"+taskJSON+"'\n")
	got, err := a.SubmitAITask("generic", "look at this", []string{"posting=/p/job.txt"}, "app-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "t1" || got.ConversationID != "c1" || got.ApplicationID != "app-1" || got.Fallback != "" {
		t.Fatalf("task = %+v", got)
	}
	want := "--profile work --json aitask submit --kind generic --goal look at this --context posting=/p/job.txt --application app-1"
	if calls := loggedArgs(t, log); strings.Join(calls, "\n") != want {
		t.Fatalf("CLI calls = %v, want %q", calls, want)
	}
}

func TestSubmitAITaskCoderDisabledIsAFallbackNotAnError(t *testing.T) {
	a, _ := aitaskApp(t, `echo '{"error":"coder mode is disabled","code":"coder_disabled","fallback":"oneshot"}'; exit 3`+"\n")
	got, err := a.SubmitAITask("generic", "g", nil, "")
	if err != nil {
		t.Fatalf("coder_disabled came back as an error: %v", err)
	}
	if got.Fallback != "oneshot" || got.Reason != "coder mode is disabled" || got.TaskID != "" {
		t.Fatalf("result = %+v", got)
	}
}

func TestSubmitAITaskOtherRefusalsStayErrors(t *testing.T) {
	a, _ := aitaskApp(t, `echo '{"error":"context refused","code":"context_source_refused"}'; exit 3`+"\n")
	if _, err := a.SubmitAITask("generic", "g", nil, ""); err == nil || !strings.Contains(err.Error(), "context refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitAITaskRefusesUnknownContextClassBeforeTheCLI(t *testing.T) {
	a, log := aitaskApp(t, "echo '"+taskJSON+"'\n")
	for _, entry := range []string{"message=/m.txt", "/bare/path", "people=/x", "posting="} {
		if _, err := a.SubmitAITask("generic", "g", []string{entry}, ""); err == nil {
			t.Fatalf("context %q was accepted", entry)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the CLI was called for a refused context")
	}
}

func TestSubmitAITaskWritesSelectionTextToProfileFolder(t *testing.T) {
	root := t.TempDir()
	a, log := aitaskApp(t, `case "$*" in
*"profile folder"*) echo '{"root_dir":"`+root+`"}';;
*) echo '`+taskJSON+`';;
esac
`)
	secret := "failed node: boom"
	if _, err := a.SubmitAITask("generic", "fix", []string{selectionTextPrefix + secret}, ""); err != nil {
		t.Fatal(err)
	}
	var submit string
	for _, c := range loggedArgs(t, log) {
		if strings.Contains(c, "aitask submit") {
			submit = c
		}
	}
	prefix := "--context selection=" + filepath.Join(root, ".monoagent", "ai-task-input") + string(filepath.Separator)
	i := strings.Index(submit, prefix)
	if i < 0 {
		t.Fatalf("submit = %q, want %q...", submit, prefix)
	}
	path := strings.TrimPrefix(submit[i:], "--context selection=")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != secret {
		t.Fatalf("file = %q, %v", b, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	if strings.Contains(submit, secret) {
		t.Fatal("the text went on the command line")
	}
}

func TestWriteAITaskInputCapsSize(t *testing.T) {
	root := t.TempDir()
	a, _ := aitaskApp(t, `echo '{"root_dir":"`+root+`"}'`+"\n")
	path, err := a.writeAITaskInput(strings.Repeat("é", maxAITaskInputBytes)) // 2 bytes each
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if len(b) > maxAITaskInputBytes || len(b) < maxAITaskInputBytes-1 {
		t.Fatalf("wrote %d bytes, cap %d", len(b), maxAITaskInputBytes)
	}
}

func TestGetAndCancelAITask(t *testing.T) {
	a, log := aitaskApp(t, "echo '"+taskJSON+"'\n")
	if got, err := a.GetAITask("t1"); err != nil || got.Status != "running" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := a.CancelAITask("t1"); err != nil {
		t.Fatal(err)
	}
	want := "--profile work --json aitask status t1\n--profile work --json aitask cancel t1"
	if calls := loggedArgs(t, log); strings.Join(calls, "\n") != want {
		t.Fatalf("CLI calls = %v", calls)
	}
}

func TestAITaskStatusReadsCoderStatus(t *testing.T) {
	for _, tc := range []struct {
		out     string
		enabled bool
		reason  string
	}{
		{`{"enabled":true,"ready":true}`, true, ""},
		{`{"enabled":false,"ready":true}`, false, "Coder mode is off"},
		{`{"enabled":true,"ready":false}`, false, "not ready"},
	} {
		a, log := aitaskApp(t, "echo '"+tc.out+"'\n")
		got, err := a.AITaskStatus()
		if err != nil || got.Enabled != tc.enabled || !strings.Contains(got.Reason, tc.reason) {
			t.Fatalf("%s -> %+v, %v", tc.out, got, err)
		}
		if calls := loggedArgs(t, log); strings.Join(calls, "\n") != "--profile work --json coder status" {
			t.Fatalf("calls = %v", calls)
		}
	}
}
