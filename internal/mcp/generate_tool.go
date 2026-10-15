package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/monomind"
)

// generateExecFor makes the model turn (a variable so tests use a fake).
var generateExecFor = func(ctx context.Context, runtime, model string) (generate.ExecFunc, error) {
	bin, _, err := monomind.Ensure(ctx)
	if err != nil {
		return nil, monomind.MarkNotSetup(err)
	}
	return generate.MonomindExec(bin, runtime, model), nil
}

// generateScan is `agent scan` for runtime "auto" (a variable for tests).
var generateScan = monomind.Scan

// generateTools is workflow_generate, the MCP side of `workflow generate`.
func generateTools() []tool {
	return []tool{{
		name: "workflow_generate",
		description: "Build a workflow from a plain-language description with a local AI agent (monomind). " +
			"The agent's workflow JSON is validated (structure, DAG, trigger, known node types) and its problems " +
			"fed back until it passes or max_attempts turns have run; then it is saved as a new INACTIVE draft " +
			"(save:false returns the JSON only). Returns {saved, id, name, node_count, valid, attempts, history, runtime, model, workflow}. " +
			"runtime \"auto\" (default) uses the first ready model of the validated roster.",
		schema: objSchema(map[string]interface{}{
			"description":  strParam("What the workflow should do"),
			"runtime":      strParam(`Agent runtime (claude, codex, ...) or "auto" (default)`),
			"model":        strParam("Model of the runtime (optional)"),
			"name":         strParam("Workflow name (default: the agent's choice)"),
			"max_attempts": map[string]interface{}{"type": "number", "description": "Turns before giving up (default 3, max 10)"},
			"save":         map[string]interface{}{"type": "boolean", "description": "Save as an inactive draft (default true)"},
		}, "description"),
		annotations: map[string]bool{"readOnlyHint": false},
		mutating:    true,
		handler:     toolWorkflowGenerate,
	}}
}

func toolWorkflowGenerate(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		Description string  `json:"description"`
		Runtime     string  `json:"runtime"`
		Model       string  `json:"model"`
		Name        string  `json:"name"`
		MaxAttempts float64 `json:"max_attempts"`
		Save        *bool   `json:"save"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Description) == "" {
		return nil, fmt.Errorf("description is required")
	}
	attempts := int(a.MaxAttempts)
	switch {
	case attempts <= 0:
		attempts = generate.DefaultMaxAttempts
	case attempts > 10:
		attempts = 10
	}
	save := a.Save == nil || *a.Save

	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	runtime, model := strings.TrimSpace(a.Runtime), a.Model
	if runtime == "" || runtime == generate.RuntimeAuto {
		results, _ := agentroster.List(ctx, rt.db.DB)
		scan, serr := generateScan(ctx)
		if serr != nil {
			scan = nil
		}
		var pickedModel string
		if runtime, pickedModel, err = generate.PickRuntime(results, scan); err != nil {
			return nil, err
		}
		if model == "" {
			model = pickedModel
		}
	}
	exec, err := generateExecFor(ctx, runtime, model)
	if err != nil {
		return nil, err
	}
	reg := rt.registryOrDefault()
	res, err := generate.Generate(ctx, generate.Request{
		Description: a.Description,
		Name:        a.Name,
		MaxAttempts: attempts,
		Catalog:     generate.BuildCatalog(append(reg.Types(), generate.TriggerTypes()...), nodeTitle, nil),
		Exec:        exec,
		Validate:    generate.Validator(reg.Has),
	})
	if err != nil {
		var ae *generate.AttemptsError
		if errors.As(err, &ae) {
			return nil, fmt.Errorf("%v (history: %s)", err, historyText(ae.History))
		}
		return nil, err
	}
	out := map[string]interface{}{
		"saved":      false,
		"name":       res.Workflow.Name,
		"node_count": len(res.Workflow.Nodes),
		"valid":      true,
		"warnings":   res.Warnings,
		"attempts":   res.Attempts,
		"history":    res.History,
		"runtime":    runtime,
		"model":      model,
		"is_active":  false,
		"workflow":   res.Document,
	}
	if save {
		saved, err := generate.SaveDraft(ctx, rt.store, rt.db.DB, profileOf(ctx), res.Workflow)
		if err != nil {
			return nil, err
		}
		out["saved"], out["id"], out["workflow"] = true, saved.ID, generate.ToFile(saved)
	}
	return out, nil
}

func historyText(h []generate.Attempt) string {
	parts := make([]string, 0, len(h))
	for _, a := range h {
		parts = append(parts, fmt.Sprintf("#%d: %s", a.N, strings.Join(a.Errors, "; ")))
	}
	return strings.Join(parts, " | ")
}
