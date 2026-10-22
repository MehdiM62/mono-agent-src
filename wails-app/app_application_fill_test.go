//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestStartApplicationFillShellsOutAndReturnsTask(t *testing.T) {
	a, log := aitaskApp(t, `echo '{"applicationId":"app-1","taskId":"t9","conversationId":"c9","status":"queued"}'`+"\n")
	got, err := a.StartApplicationFill("app-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "t9" || got.ConversationID != "c9" || got.ApplicationID != "app-1" || got.Fallback != "" || got.Goal == "" {
		t.Fatalf("task = %+v", got)
	}
	want := "--profile work --json application fill start app-1"
	if calls := loggedArgs(t, log); strings.Join(calls, "\n") != want {
		t.Fatalf("CLI calls = %v, want %q", calls, want)
	}
}

func TestStartApplicationFillCoderDisabledIsAFallback(t *testing.T) {
	a, _ := aitaskApp(t, `echo '{"error":"coder mode is disabled","code":"coder_disabled","fallback":"oneshot"}'; exit 3`+"\n")
	got, err := a.StartApplicationFill("app-1")
	if err != nil || got.Fallback != "oneshot" || got.Reason != "coder mode is disabled" || got.TaskID != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestStartApplicationFillOtherRefusalsStayErrors(t *testing.T) {
	a, _ := aitaskApp(t, `echo '{"error":"already has an open task","code":"task_open"}'; exit 3`+"\n")
	if _, err := a.StartApplicationFill("app-1"); err == nil || !strings.Contains(err.Error(), "open task") {
		t.Fatalf("err = %v", err)
	}
}
