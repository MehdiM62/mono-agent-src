package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Any command with --json prints its failure as one JSON object on stdout,
// with the code of its exit class; human mode prints nothing on stdout.
func TestReportCommandErrorJSONForEveryCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reportCommandError([]string{"secret", "rm", "x", "--json"}, errNotFound("no secret named %q found", "x"), &stdout, &stderr)
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil || doc["error"] != `no secret named "x" found` || doc["code"] != "not_found" {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	reportCommandError([]string{"secret", "rm", "x"}, errNotFound("gone"), &stdout, &stderr)
	if stdout.Len() != 0 {
		t.Errorf("human mode wrote stdout: %q", stdout.String())
	}

	stdout.Reset()
	reportCommandError([]string{"--json", "node", "list"}, errors.New("boom"), &stdout, &stderr)
	if strings.TrimSpace(stdout.String()) != `{"error":"boom"}` {
		t.Errorf("plain error: stdout = %q", stdout.String())
	}
}

// withJSONErrors prints the error object itself, so main must not add a
// second one: stdout stays exactly one document. The exit code is kept.
func TestWithJSONErrorsPrintsOneDocument(t *testing.T) {
	cfg := &globalConfig{JSONOutput: true}
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return errInvalidInput("bad") }}
	withJSONErrors(cfg, cmd)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	err := cmd.RunE(cmd, nil)
	reportCommandError([]string{"--json", "x"}, err, &stdout, &bytes.Buffer{})
	if got := strings.TrimSpace(stdout.String()); got != `{"code":"invalid_input","error":"bad"}` {
		t.Fatalf("stdout = %q", got)
	}
	if exitCodeFor(err) != 3 {
		t.Errorf("exit %d, want 3", exitCodeFor(err))
	}
}
