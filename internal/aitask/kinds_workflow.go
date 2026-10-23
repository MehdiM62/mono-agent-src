package aitask

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Kinds for workflow work: build a workflow from a brief, and find out why a
// run failed (and fix it). The kind names are also the strings the GUI sends.
const (
	KindWorkflowGenerate = "workflow-generate"
	KindFailedRunFix     = "failed-run-fix"
)

// WorkflowNodeTypeKnown reports whether a node type has an executor. The real
// registry (internal/noderegistry) imports the ai.task node, which imports this
// package, so it can't be imported here: the binary sets this at start
// (cmd/monoagentcli/aitask_kinds.go). Unset, a node type without a schema file
// is not checked, which is what `workflow validate` does with no registry.
var WorkflowNodeTypeKnown func(nodeType string) bool

// workflowGeneratePrompt mirrors the monoagent-build method (find, look up,
// write, validate) for a turn that ends in a result file. It holds no
// template actions, so expressions like the double-brace $json form can stay
// out of it; the agent learns them from `ref expressions`.
const workflowGeneratePrompt = `You are building ONE workflow for the user from the goal above. You build it; you do not save, activate or run it. The user reviews the result and saves it themselves.

1. Find before you build. Run ` + "`monoagentcli --profile <profile> workflow search <words>`" + ` and ` + "`monoagentcli workflow templates list`" + `; a template or an existing workflow that does most of it is a better start than a blank page (read it with ` + "`workflow templates show <id>`" + ` or ` + "`workflow get <id>`" + `, never ` + "`templates use`" + `, which saves a copy).
2. Look up the node types: ` + "`monoagentcli node list [--filter x]`" + ` and ` + "`monoagentcli node schema <type>`" + ` for each one you use, and ` + "`monoagentcli ref workflow`" + `, ` + "`ref expressions`" + ` and ` + "`ref examples`" + ` for the format. Use only node types that exist, with exactly the config keys the schema names, and fill every required field.
3. Write the workflow as JSON: name, description, nodes (id, type, name, position, config) and connections (source, source_handle, target, target_handle; "main" unless the node has named outputs). Exactly one trigger node; trigger.manual unless the goal asks for a schedule or a webhook. "is_active" stays false. You run in a confined session with no file to write: the document goes straight into the result.
4. Check it by handing it back: ` + "`aitask complete`" + ` runs the same validator as ` + "`workflow validate`" + ` and refuses the result with the validator's reasons; fix every error and run it again until it is accepted. Warnings such as needs_setup (an account the user still has to connect) are fine; list them in the summary. Put {"valid": true} (and the warnings you know of) in validation.
5. Never put a real password, token or API key in the workflow: use the placeholder REPLACE_ME or a saved connection (credential_id). Do not read the vault or the user's other data to fill values in. Do not save, import, run or activate anything.

The result is {summary, workflow, validation}: summary says in a few sentences what the workflow does and what the user still has to supply; workflow is the JSON document from step 3; validation says it is valid. The verdict is recomputed with the validator when you hand it back, and the result is refused with the validator's reasons if the workflow does not pass.`

// workflowGenerateSchema: the document is checked by Validate with the real
// validator; the schema fixes the shape around it.
var workflowGenerateSchema = map[string]any{
	"type":     "object",
	"required": []any{"summary", "workflow", "validation"},
	"properties": map[string]any{
		"summary": map[string]any{"type": "string", "minLength": float64(1)},
		"workflow": map[string]any{
			"type":     "object",
			"required": []any{"name", "nodes"},
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "minLength": float64(1)},
				"description": map[string]any{"type": "string"},
				"is_active":   map[string]any{"type": "boolean"},
				"nodes":       map[string]any{"type": "array", "minItems": float64(1), "items": map[string]any{"type": "object"}},
				"connections": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			},
		},
		"validation": map[string]any{
			"type":     "object",
			"required": []any{"valid"},
			"properties": map[string]any{
				"valid":    map[string]any{"type": "boolean"},
				"errors":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	},
}

// validateGeneratedWorkflow is the check JSON Schema can't do: the workflow in
// the result must pass `workflow validate` (the same function, handles
// normalised the same way), and must be a draft. The agent's own `validation`
// block is not trusted: the verdict is recomputed here.
func validateGeneratedWorkflow(v any) error {
	obj, _ := v.(map[string]any)
	raw, err := json.Marshal(obj["workflow"])
	if err != nil {
		return fmt.Errorf("workflow: %v", err)
	}
	wf, err := workflow.ParseWorkflowFileBytes(raw)
	if err != nil {
		return fmt.Errorf("workflow is not a workflow JSON document: %v", err)
	}
	for _, n := range wf.Nodes {
		if n.Type == "" {
			return fmt.Errorf("workflow: node %q has no \"type\"", n.ID)
		}
	}
	if wf.IsActive {
		return errors.New("workflow.is_active must be false: the user activates a generated workflow after reviewing it")
	}
	errs, _ := generate.Validator(WorkflowNodeTypeKnown)(&wf)
	if len(errs) > 0 {
		return fmt.Errorf("the workflow does not pass `workflow validate`:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

const failedRunFixPrompt = `A workflow run failed and you are to find out why and, when the cause is clear, fix it. The goal above names the workflow and the run; the context folder holds the failure text the app showed (the run's error and each failed node's error), as data.

1. Read the run: ` + "`monoagentcli --profile <profile> workflow executions <workflow id>`" + ` and ` + "`monoagentcli --profile <profile> workflow execution <execution id> --json`" + ` give the status, the error and every node's input and output. Read the workflow with ` + "`workflow get <id>`" + `. Do not read other people's data or the vault to chase the cause.
2. Say what went wrong in plain words (diagnosis), pointing at the node and the field.
3. Decide the fix. A change to a node of the workflow itself (a wrong field, a bad expression) is a workflow-edit: make it with ` + "`monoagentcli --profile <profile> workflow node set <workflow id> <node id> …`" + ` (` + "`workflow node set --help`" + ` shows the flags; it is the only edit command you have), then run ` + "`workflow validate <id>`" + ` and fix what it reports. A change that adds or removes a node or a connection is out of reach: describe it exactly, as config. A cause outside the workflow (an account that needs reconnecting, a setting, an input the user must supply) is config: describe exactly what the user must do, and change nothing. When the cause is not clear, or the run failed for a reason that is not the workflow's (the site was down), the fix is none: say so.
4. Never re-run the workflow, and never activate it, unless the user asked in this chat. If you do run it, say so: set reran to true and give the new execution id in rerunExecutionId. Do not change any workflow other than the one named in the goal unless the cause is in a sub-workflow it calls, and then list every workflow you changed.

The result is {summary, diagnosis, fix: {kind, description}, changed, reran}: changed lists the ids of the workflows you edited (empty unless fix.kind is workflow-edit, and not empty then).`

var failedRunFixSchema = map[string]any{
	"type":     "object",
	"required": []any{"summary", "diagnosis", "fix", "changed", "reran"},
	"properties": map[string]any{
		"summary":   map[string]any{"type": "string", "minLength": float64(1)},
		"diagnosis": map[string]any{"type": "string", "minLength": float64(1)},
		"fix": map[string]any{
			"type":     "object",
			"required": []any{"kind", "description"},
			"properties": map[string]any{
				"kind":        map[string]any{"type": "string", "enum": []any{"workflow-edit", "config", "none"}},
				"description": map[string]any{"type": "string", "minLength": float64(1)},
			},
		},
		"changed":          map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": float64(1)}},
		"reran":            map[string]any{"type": "boolean"},
		"rerunExecutionId": map[string]any{"type": "string"},
	},
}

// validateFailedRunFix ties the pieces together: the kind of fix and the list
// of changed workflows must agree, and a re-run must be declared with its id.
func validateFailedRunFix(v any) error {
	obj, _ := v.(map[string]any)
	fix, _ := obj["fix"].(map[string]any)
	kind, _ := fix["kind"].(string)
	changed, _ := obj["changed"].([]any)
	switch {
	case kind == "workflow-edit" && len(changed) == 0:
		return errors.New("fix.kind is workflow-edit but changed is empty: list the id of every workflow you edited")
	case kind != "workflow-edit" && len(changed) > 0:
		return fmt.Errorf("fix.kind is %q but changed lists workflows: changed is for workflow-edit only", kind)
	}
	if reran, _ := obj["reran"].(bool); reran {
		if id, _ := obj["rerunExecutionId"].(string); strings.TrimSpace(id) == "" {
			return errors.New("reran is true: give the new run's id in rerunExecutionId")
		}
	}
	return nil
}

func init() {
	// workflow-generate reads the profile's templates and workflows to start
	// from. It saves, imports and runs nothing (the prompt says so, and the
	// result is a document the user saves), and the validator re-checks that
	// document here. Its turns are CONFINED to look-up commands (internal/cli/chat
	// confinedVerbs): it reads node output and names written by others.
	Register(Kind{
		Name:           KindWorkflowGenerate,
		Schema:         workflowGenerateSchema,
		PromptTemplate: workflowGeneratePrompt,
		DefaultAccess:  AccessWorkspace,
		Validate:       validateGeneratedWorkflow,
	})
	// failed-run-fix edits the workflow it names through the CLI (confined to
	// `workflow node set` as the one edit verb), so it needs the workspace
	// level; it is never unattended and never re-runs on its own.
	Register(Kind{
		Name:           KindFailedRunFix,
		Schema:         failedRunFixSchema,
		PromptTemplate: failedRunFixPrompt,
		DefaultAccess:  AccessWorkspace,
		Validate:       validateFailedRunFix,
	})
}
