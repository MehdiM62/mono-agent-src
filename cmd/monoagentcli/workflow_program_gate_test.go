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

// The chat builds and activates a workflow that writes ~/.bashrc: until the
// user approves it, the run refuses the write (a hidden file in the home
// runs code later) and says so; once the user activates it, it writes.
func TestAWorkflowTheChatBuiltWritesNoHiddenHomeFileUntilApproved(t *testing.T) {
	home, cfg := persistTestEnv(t)
	asOperatorProcess(t)
	rc := filepath.Join(home, ".bashrc")
	stepCfg, _ := json.Marshal(map[string]interface{}{"file_path": rc, "field": "text", "encoding": "utf8"})
	setCfg, _ := json.Marshal(map[string]interface{}{"assignments": []map[string]interface{}{{"field": "text", "value": "echo hi"}}})

	t.Setenv(browserctl.EnvAICaller, "chat") // the chat writes and activates it
	var created workflow.Workflow
	if err := json.Unmarshal([]byte(runWorkflowSubcmd(t, cfg, "create", "chat-built-rc")), &created); err != nil {
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
	set := add("core.set", "Text", "--config", string(setCfg))
	step := add("data.write_binary_file", "Write", "--config", string(stepCfg))
	runWorkflowSubcmd(t, cfg, "connect", created.ID, "--from", trig, "--to", set)
	runWorkflowSubcmd(t, cfg, "connect", created.ID, "--from", set, "--to", step)
	runWorkflowSubcmd(t, cfg, "activate", created.ID)
	t.Setenv(browserctl.EnvAICaller, "")

	cfg.JSONOutput = false
	_, stdout, _ := runLogsFor(t, cfg, created.ID)
	if !strings.Contains(stdout, "hidden file or folder") {
		t.Errorf("the unapproved run does not say why the write was refused:\n%s", stdout)
	}
	if _, err := os.Stat(rc); err == nil {
		t.Fatal("a workflow the chat built wrote ~/.bashrc before the user approved it")
	}
	cfg.JSONOutput = true
	runWorkflowSubcmd(t, cfg, "activate", created.ID) // the user approves it
	cfg.JSONOutput = false
	if _, stdout, err := runLogsFor(t, cfg, created.ID); err != nil {
		t.Fatalf("the approved run: %v\n%s", err, stdout)
	}
	if b, err := os.ReadFile(rc); err != nil || string(b) != "echo hi" {
		t.Fatalf("the approved workflow's write: %q, %v", b, err)
	}
}
