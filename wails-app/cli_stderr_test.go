package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestCLIStderrDropsAdvisoryLines(t *testing.T) {
	in := "A monoes.me login will be required from 2099-01-01: monoagentcli account login\n" +
		`{"level":"warn","error":"webhook server: listen on 127.0.0.1:9321: address already in use"}` + "\n" +
		"listing browsers: the bridge rejected this client: pairing token mismatch\n"
	if got, want := cliStderr([]byte(in)), "listing browsers: the bridge rejected this client: pairing token mismatch"; got != want {
		t.Fatalf("cliStderr = %q, want %q", got, want)
	}
	if got := cliStderr([]byte("A monoes.me login will be required from 2099-01-01: monoagentcli account login\n")); got != "" {
		t.Fatalf("warning-only stderr = %q, want empty", got)
	}
	if got := cliStderr([]byte("line one\nline two\n")); got != "line one\nline two" {
		t.Fatalf("plain stderr changed: %q", got)
	}
}

func TestCLIResultJSONStripsWarningFromError(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("A monoes.me login will be required from 2099-01-01: monoagentcli account login\nboom: it broke\n")}
	var out struct {
		Error string `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(cliResultJSON("monoagentcli", nil, err)), &out); jerr != nil {
		t.Fatal(jerr)
	}
	if out.Error != "boom: it broke" {
		t.Fatalf("error = %q, want only the real error", out.Error)
	}
}
