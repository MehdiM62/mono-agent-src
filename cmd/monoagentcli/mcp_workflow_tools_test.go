package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/mcp"
)

// --workflow-tools alone serves every workflow, with a value only those listed;
// it is handed to the server and refused with the narrow modes and grant mode.
func TestMCPCommandHandsWorkflowToolsToTheServer(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--workflow-tools"}, "all"},
		{[]string{"--workflow-tools", "--allow-mutations"}, "all"},
		{[]string{"--workflow-tools=wf-1,Daily digest"}, "wf-1,Daily digest"},
	} {
		got, err := runMCPCommand(t, c.args...)
		if err != nil || got == nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if got.WorkflowTools != c.want {
			t.Errorf("%v: WorkflowTools %q, want %q", c.args, got.WorkflowTools, c.want)
		}
	}
	for _, args := range [][]string{
		{"--workflow-tools", "--api-only"},
		{"--workflow-tools", "--tasks-only"},
	} {
		got, err := runMCPCommand(t, args...)
		if got != nil || !errors.Is(err, mcp.ErrWorkflowToolsWithNarrowMode) {
			t.Errorf("%v: options %v, error %v", args, got, err)
		}
	}
	got, err := runMCPCommand(t, "--grant", "grt_x", "--workflow-tools")
	if got != nil || err == nil || !strings.Contains(err.Error(), "--workflow-tools") {
		t.Errorf("--grant with --workflow-tools: options %v, error %v", got, err)
	}
}
