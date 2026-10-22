package main

import "testing"

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
