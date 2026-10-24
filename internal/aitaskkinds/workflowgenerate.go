package aitaskkinds

import "github.com/monoes/mono-agent/internal/aitask"

func workflowGenerateKind() aitask.Kind {
	return aitask.Kind{
		Name:          WorkflowGenerate,
		DefaultAccess: aitask.AccessWorkspace,
		Schema: map[string]any{
			"type":                 "object",
			"required":             []any{"workflowId"},
			"additionalProperties": false,
			"properties": map[string]any{
				"workflowId": map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(maxID)},
				"name":       str(maxName),
				"summary":    str(maxText),
				"nodeCount":  map[string]any{"type": "integer"},
				"validation": map[string]any{
					"type":                 "object",
					"required":             []any{"ok"},
					"additionalProperties": false,
					"properties": map[string]any{
						"ok":     map[string]any{"type": "boolean"},
						"errors": strList(maxListLen, maxListStr),
					},
				},
			},
		},
		PromptTemplate: `Build the workflow the brief (the goal and the selection* files) describes, and hand it back as a draft. ` + dataRule + `

1. Learn the nodes before using them: ` + "`monoagentcli node list`, `monoagentcli node schema <type>`, `monoagentcli ref [topic]`" + ` and the existing
   workflows and templates (` + "`workflow list`, `workflow search <words>`, `workflow templates`" + `). Use the same --profile as the complete command below.
2. Write the workflow JSON in your working folder, check it with ` + "`monoagentcli --profile P workflow validate --file <file>`" + ` until it is valid, and
   save it with ` + "`workflow import --file <file>`" + ` (or ` + "`workflow create`, `workflow node add` and `workflow connect`" + `). It is saved inactive, and it
   counts as AI-authored until the user approves it in the app. Validate the saved workflow by id once more.
3. NEVER run, activate or schedule it. If the brief asks for a step that sends, posts, publishes or deletes (an email, a message, a post, a
   write to an outside service), say so in the summary: name the node and what it sends, so the user sees it before approving.
4. Hand back workflowId (the id the CLI printed, never one you made up: the result is refused when the profile has no such workflow), name,
   summary (what the workflow does and which steps send something), nodeCount and validation {ok, errors}.`,
	}
}
