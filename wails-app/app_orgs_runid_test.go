package main

import (
	"os/exec"
	"testing"
)

// org:runStatus carries runId when the run is known and omits it otherwise;
// the existing fields stay as they were.
func TestOrgRunStatusEventCarriesRunIDOnlyWhenKnown(t *testing.T) {
	ev := orgRunStatusEvent("acme", "stopped", "", "r-42")
	if ev["runId"] != "r-42" || ev["orgName"] != "acme" || ev["status"] != "stopped" {
		t.Errorf("event = %v", ev)
	}
	if _, ok := ev["message"]; !ok {
		t.Errorf("terminal event lost its message field: %v", ev)
	}
	run := orgRunStatusEvent("acme", "running", "", "")
	if _, ok := run["runId"]; ok {
		t.Errorf("unknown run id is not omitted: %v", run)
	}
	if _, ok := run["message"]; ok {
		t.Errorf("running event gained a message: %v", run)
	}
}

func TestRunIDFromStatus(t *testing.T) {
	for out, want := range map[string]string{
		`{"status":"running","run":"r-42","pid":7}`: "r-42",
		`{"status":"stopped"}`:                      "",
		`{"error":"no such org"}`:                   "",
		`not json`:                                  "",
	} {
		if got := runIDFromStatus(out); got != want {
			t.Errorf("runIDFromStatus(%s) = %q, want %q", out, got, want)
		}
	}
}

// A poll result that arrives after the terminal event emits nothing; one that
// arrives while the run is still registered emits running with the run id.
func TestOrgRunPollNeverEmitsRunningAfterTheTerminalEvent(t *testing.T) {
	a := newTestApp(t)
	cmd := &exec.Cmd{}
	a.runningCmds["orgrun:acme"] = cmd
	var got []map[string]interface{}
	emit := func(ev map[string]interface{}) { got = append(got, ev) }

	if !a.emitOrgRunning("orgrun:acme", cmd, "acme", "r-42", emit) {
		t.Fatal("a poll result while still running was not emitted")
	}
	a.finishOrgRun("orgrun:acme", cmd, orgRunStatusEvent("acme", "stopped", "", "r-42"), emit)
	if a.emitOrgRunning("orgrun:acme", cmd, "acme", "r-42", emit) {
		t.Error("a poll result after the terminal event was emitted")
	}
	if len(got) != 2 || got[0]["status"] != "running" || got[0]["runId"] != "r-42" || got[1]["status"] != "stopped" {
		t.Errorf("events = %v", got)
	}
	if _, ok := a.runningCmds["orgrun:acme"]; ok {
		t.Error("terminal path left the registration")
	}

	// A new run under the same key is a different command: the old poll stays silent.
	a.runningCmds["orgrun:acme"] = &exec.Cmd{}
	if a.emitOrgRunning("orgrun:acme", cmd, "acme", "r-42", emit) {
		t.Error("a poll for an earlier command emitted for the new run")
	}
}
