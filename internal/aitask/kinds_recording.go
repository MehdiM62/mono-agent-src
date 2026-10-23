package aitask

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/recordanalyze"
)

// KindRecordingAnalysis turns a browser recording (and the draft the one-shot
// analysis made from it) into an automation: an action with named selectors.
const KindRecordingAnalysis = "recording-analysis"

const recordingPrologue = "You are given a browser recording and the draft automation the one-shot analysis made from it. " +
	"The draft folder is in the context folder (automation.json, actions/*.json, selectors.json, fragments/, draft.json); " +
	"the recording itself can be read with `monoagentcli --profile <profile> record show <recording id> --json` (the id is in the context README or the goal). " +
	"Produce the best automation for what the user was doing: check the steps and inputs, repair selectors that would not " +
	"hold (prefer a stable id, data-testid or aria name over a position), name things well, and keep the draft where it is already right.\n\n" +
	"Hard rules: never write a credential, token, cookie or personal value of the recorded session anywhere; values the " +
	"run needs become inputs (`{{name}}`, secrets `{{secret:<name>}}`). The result file is NOT the bare JSON object the " +
	"guide below ends with: it is that object's `automation`, `action`, `selectors`, `fragments` and `scripts` plus a " +
	"`summary` (what you changed and why), optional `notes` and `risks` (arrays of short strings: what could break, what the user must check). " +
	"The command checks the automation, the action and every selector with the same validators `record analyze` uses, " +
	"and refuses the file with the reasons if any fails.\n\n--- guide ---\n"

// recordingTemplate is the prologue plus the record-to-action guide, with every
// literal "{{" escaped for text/template. If the guide can't be read
// the prologue alone stands.
func recordingTemplate() string {
	text := recordingPrologue
	if guide, err := recordanalyze.Skill(); err == nil {
		text += guide
	}
	return strings.ReplaceAll(text, "{{", `{{"{{"}}`)
}

func recordingSchema() map[string]any {
	str := map[string]any{"type": "string"}
	strs := map[string]any{"type": "array", "items": str}
	return map[string]any{
		"type":     "object",
		"required": []any{"summary", "automation", "action", "selectors"},
		"properties": map[string]any{
			"summary": map[string]any{"type": "string", "minLength": float64(1)},
			"automation": map[string]any{"type": "object", "required": []any{"id"},
				"properties": map[string]any{"id": str, "name": str, "description": str}},
			"action": map[string]any{"type": "object", "required": []any{"actionType", "steps"},
				"properties": map[string]any{"actionType": str, "description": str, "sideEffects": str,
					"steps": map[string]any{"type": "array", "minItems": float64(1), "items": map[string]any{"type": "object"}}}},
			"selectors": map[string]any{"type": "object"},
			"fragments": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"scripts":   map[string]any{"type": "object"},
			"notes":     strs,
			"risks":     strs,
		},
	}
}

// checkRecordingResult runs the repo's own validators over a recording-analysis
// result: the same CheckOutput `record analyze` uses (shape, names, selector
// references, intents, step vocabulary, action.Validate against the draft
// package, advanced steps refused) plus the lint rules that need no recorded
// DOM (a literal credential, a token in a URL). Unknown top-level keys are
// refused so a misspelled "selectors" is not silently ignored.
func checkRecordingResult(result any) error {
	obj, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("the result must be a JSON object")
	}
	draft := map[string]any{}
	for k, v := range obj {
		switch k {
		case "summary", "notes", "risks":
		default:
			draft[k] = v
		}
	}
	b, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	var out recordanalyze.Output
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return fmt.Errorf("the automation does not decode: %v", err)
	}
	env := &recordanalyze.Env{}
	problems := recordanalyze.CheckOutput(&out, env)
	m := recordanalyze.BuildManifest(&out, env)
	for _, is := range recordanalyze.Lint(&out, env, m, nil) {
		switch is.Code {
		case "literal_secret", "token_in_url", "off_domain":
			if is.Severity == "error" {
				problems = append(problems, fmt.Sprintf("%s (step %q): %s", is.Code, is.StepID, is.Message))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	const maxShown = 12
	more := ""
	if len(problems) > maxShown {
		more = fmt.Sprintf("; and %d more", len(problems)-maxShown)
		problems = problems[:maxShown]
	}
	return fmt.Errorf("%s%s", strings.Join(problems, "; "), more)
}

func init() {
	Register(Kind{
		Name:           KindRecordingAnalysis,
		Schema:         recordingSchema(),
		PromptTemplate: recordingTemplate(),
		// The input is page content captured from sites the user visited
		// (selectors, DOM-derived intents, URLs): hostile text. The agent
		// reads the context and hands a JSON result back; it changes nothing.
		// Its turns are CONFINED (internal/cli/chat confinedVerbs): `record show`
		// and `aitask complete|fail` are all it can run.
		DefaultAccess: AccessQA,
		Validate:      checkRecordingResult,
	})
}
