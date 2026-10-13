package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/automation"
	"github.com/monoes/mono-agent/internal/workflow"
)

// hintError is a failure that says how to fix it. Its text is the message
// and the hint ("… — <hint>"); with --json the hint is also its own field.
// The cause classifies it: exit code and JSON code come from err.
type hintError struct {
	err  error
	msg  string
	hint string
}

func (e *hintError) Error() string { return e.msg + " — " + e.hint }
func (e *hintError) Unwrap() error { return e.err }

func (e *hintError) JSONErrorFields() map[string]any {
	return map[string]any{"hint": e.hint}
}

// workflowNotFound turns the engine's "trigger workflow: engine: trigger
// workflow: workflow: workflow not found" into one sentence and a next step.
// Any other error is returned as it is.
func workflowNotFound(err error, id string) error {
	if !errors.Is(err, workflow.ErrWorkflowNotFound) {
		return err
	}
	return &hintError{err: err, msg: fmt.Sprintf("workflow %q not found", id), hint: "list them with: monoagentcli workflow list"}
}

// automationInstallHint says how to get node types that are not installed.
// Web automations are library packages since they stopped being embedded;
// a type's automation id is its prefix (gemini.generate_image → gemini). A
// built-in namespace is never an automation, so it only gets the listing.
func automationInstallHint(types []string) string {
	seen := map[string]bool{}
	var ids []string
	for _, t := range types {
		id, _, ok := strings.Cut(t, ".")
		if !ok || id == "" || automation.ReservedID(id) || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	switch len(ids) {
	case 0:
		return "see the automations you can install with: monoagentcli library list --kind automation"
	case 1:
		return fmt.Sprintf("this needs the %q web automation: monoagentcli library install automation %s", ids[0], ids[0])
	}
	cmds := make([]string, len(ids))
	for i, id := range ids {
		cmds[i] = "monoagentcli library install automation " + id
	}
	return "this needs the web automations " + strings.Join(ids, ", ") + ": " + strings.Join(cmds, "; ")
}

// unknownNodeTypePattern finds the type in the engine's "workflow: unknown
// node type: gemini.generate_image" (an execution's error message is text,
// so the sentinel cannot be matched with errors.Is there).
var unknownNodeTypePattern = regexp.MustCompile(`unknown node type:?\s*"?([A-Za-z0-9_.\-]+)`)

// withNodeTypeHint adds the install hint to a run failure caused by a node
// type that is not installed; any other error is returned as it is.
func withNodeTypeHint(err error) error {
	if err == nil {
		return nil
	}
	m := unknownNodeTypePattern.FindAllStringSubmatch(err.Error(), -1)
	if len(m) == 0 {
		return err
	}
	types := make([]string, 0, len(m))
	for _, g := range m {
		types = append(types, g[1])
	}
	return &hintError{err: err, msg: err.Error(), hint: automationInstallHint(types)}
}

// missingTemplateNodeTypes lists a template's node types that has does not
// know, sorted. Triggers are the engine's own and never in a registry.
func missingTemplateNodeTypes(tmpl workflow.WorkflowFile, has func(string) bool) []string {
	seen := map[string]bool{}
	var missing []string
	for _, n := range tmpl.Nodes {
		if strings.HasPrefix(n.Type, "trigger.") || seen[n.Type] || has(n.Type) {
			continue
		}
		seen[n.Type] = true
		missing = append(missing, n.Type)
	}
	sort.Strings(missing)
	return missing
}

// checkTemplateNodeTypes fails before anything is created when a template
// uses node types this install does not have (e.g. an automation that is
// no longer bundled), naming them and how to install them.
func checkTemplateNodeTypes(id string, tmpl workflow.WorkflowFile, has func(string) bool) error {
	missing := missingTemplateNodeTypes(tmpl, has)
	if len(missing) == 0 {
		return nil
	}
	return &hintError{
		err:  workflow.ErrNodeTypeUnknown,
		msg:  fmt.Sprintf("template %q uses node types that are not installed: %s", id, strings.Join(missing, ", ")),
		hint: automationInstallHint(missing),
	}
}

// templateInputKeys returns the trigger-data keys a bundled template reads.
func templateInputKeys(id string) []string {
	for _, t := range workflow.ListTemplates() {
		if t.ID == id {
			return t.Inputs
		}
	}
	return nil
}

// checkTemplateInputs fails before a template runs when it reads input keys
// and none were given. Templates cannot mark a key optional, so only an
// absent or empty --input is refused, naming every key.
func checkTemplateInputs(keys []string, triggerData map[string]interface{}) error {
	if len(keys) == 0 || len(triggerData) > 0 {
		return nil
	}
	return &hintError{
		err:  ErrInvalidInput,
		msg:  "missing input: " + strings.Join(keys, ", "),
		hint: fmt.Sprintf("run with --input '%s'", exampleInputJSON(keys)),
	}
}

// nothingSucceeded fails a run that ended SUCCESS_WITH_ERRORS without a
// single non-trigger node succeeding: on_error=continue kept the run going,
// but nothing it was meant to do happened. A partial success stays a
// success (exit 0).
func nothingSucceeded(s executionSummaryJSON) error {
	if s.Status != "SUCCESS_WITH_ERRORS" {
		return nil
	}
	for _, n := range s.Nodes {
		if n.Status == "SUCCESS" && !strings.HasPrefix(n.Type, "trigger.") {
			return nil
		}
	}
	return fmt.Errorf("execution %s: every step failed (status SUCCESS_WITH_ERRORS): %s", s.ExecutionID, s.Error)
}
