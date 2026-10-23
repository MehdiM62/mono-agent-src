package aitaskkinds

import "github.com/monoes/mono-agent/internal/aitask"

func recordingAnalyzeKind() aitask.Kind {
	return aitask.Kind{
		Name:          RecordingAnalyze,
		DefaultAccess: aitask.AccessWorkspace,
		Schema: map[string]any{
			"type":                 "object",
			"required":             []any{"recordingId", "summary", "saved", "verified"},
			"additionalProperties": false,
			"properties": map[string]any{
				"recordingId":    map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(maxID)},
				"automationName": str(maxName),
				"summary":        str(maxText),
				// `record save` and `record verify` are the user's own commands
				// (an agent is refused with operator_only), so a result cannot
				// claim either.
				"saved":    map[string]any{"type": "boolean", "enum": []any{false}},
				"verified": map[string]any{"type": "boolean", "enum": []any{false}},
			},
		},
		PromptTemplate: `Analyze the recording named in the goal (selection* holds only its id and title). ` + dataRule + `

A recording holds the pages the user visited, so you never read its content: do not run ` + "`record show`, `record api` or `record replay`" + `, and do
not open files under the recordings or recording-drafts folders. The analyzer reads it for you, with no tools of its own.

1. Run ` + "`monoagentcli --profile P record analyze <recording-id> --json`" + ` (the same --profile as the complete command below). It prints the draft's
   path, the action, the target automation and the lint findings.
2. Review the findings and say in plain words what the draft does, which inputs it takes, and what is wrong or risky (lint errors, steps that
   change a site). Do not edit the draft's files.
3. You cannot save or verify it: ` + "`record save` and `record verify`" + ` are the user's own and refuse an agent. Tell the user in the summary to
   verify and save it from the app or their terminal, naming the draft path.
4. Hand back recordingId, automationName (the draft's target, when it has one), summary, saved false and verified false.`,
	}
}
