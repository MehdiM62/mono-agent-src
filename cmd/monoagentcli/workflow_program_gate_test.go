package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/workflow"
)

// The chat builds and activates a workflow with a command step: until the
// user approves it, a run (the user's own `workflow run`, which doesn't
// approve, or its schedule) refuses that step and says how to approve the
// workflow. Once the user activates it, the same run runs the command.
func TestAWorkflowTheChatBuiltRunsNoCommandUntilApproved(t *testing.T) {
	touch, err := exec.LookPath("touch")
	if err != nil {
		t.Skip("uses touch")
	}
	_, cfg := persistTestEnv(t)
	asOperatorProcess(t)
	ran := filepath.Join(t.TempDir(), "ran")
	cmdCfg, _ := json.Marshal(map[string]interface{}{"command": touch, "args": []string{ran}})

	t.Setenv(browserctl.EnvAICaller, "chat") // the chat writes it
	var created workflow.Workflow
	if err := json.Unmarshal([]byte(runWorkflowSubcmd(t, cfg, "create", "chat-built")), &created); err != nil {
		t.Fatal(err)
	}
	add := func(typ, name string, extra ...string) string {
		var n workflow.WorkflowNode
		args := append([]string{"node", "add", created.ID, "--type", typ, "--name", name}, extra...)
		if err := json.Unmarshal([]byte(runWorkflowSubcmd(t, cfg, args...)), &n); err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	trig := add("trigger.manual", "Start")
	step := add("system.execute_command", "Touch", "--config", string(cmdCfg))
	runWorkflowSubcmd(t, cfg, "connect", created.ID, "--from", trig, "--to", step)
	runWorkflowSubcmd(t, cfg, "activate", created.ID)
	t.Setenv(browserctl.EnvAICaller, "")

	cfg.JSONOutput = false
	_, stdout, _ := runLogsFor(t, cfg, created.ID)
	if !strings.Contains(stdout, "runs a program on this computer") || !strings.Contains(stdout, "workflow activate "+created.ID) {
		t.Errorf("the unapproved run does not say why the step was refused:\n%s", stdout)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a workflow the chat built ran a command before the user approved it")
	}

	cfg.JSONOutput = true
	runWorkflowSubcmd(t, cfg, "activate", created.ID) // the user approves it
	cfg.JSONOutput = false
	if _, stdout, err := runLogsFor(t, cfg, created.ID); err != nil {
		t.Fatalf("the approved run: %v\n%s", err, stdout)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Fatalf("the approved workflow's command did not run: %v", err)
	}
}
