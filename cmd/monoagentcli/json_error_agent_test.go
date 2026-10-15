package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
)

func TestJSONErrorCodeAgentNotSetup(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&monomind.ErrNotFound{Tried: []string{"/x"}}, "agent_not_setup"},
		{&monomind.ProtocolError{Code: monomind.ErrMissingBinary, Message: "codex CLI not found", ExitCode: 1}, "agent_not_setup"},
		{&monomind.ProtocolError{Code: monomind.ErrQuota, Message: "usage limit", ExitCode: 1}, ""},
		{errNotFound("workflow %q not found", "x"), "not_found"},
		{errors.New("boom"), ""},
	}
	for _, c := range cases {
		if got := jsonErrorCode(c.err); got != c.want {
			t.Errorf("jsonErrorCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// An org command's JSON error names agent_not_setup; its other failures
// keep {"error"} alone.
func TestReportCommandErrorOrgAgentNotSetup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reportCommandError([]string{"--json", "org", "run", "growth"}, &monomind.ErrNotFound{Tried: []string{"/x"}}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), `"code":"agent_not_setup"`) {
		t.Errorf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	reportCommandError([]string{"--json", "org", "status"}, errors.New("boom"), &stdout, &stderr)
	if strings.TrimSpace(stdout.String()) != `{"error":"boom"}` {
		t.Errorf("stdout = %q", stdout.String())
	}
}
