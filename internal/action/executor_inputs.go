package action

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog"
)

// seedVariables populates the execution context with initial values from the
// storage action.
func (ae *ActionExecutor) seedVariables(action *StorageAction) {
	ae.execCtx.SetVariable("actionId", action.ID)
	ae.execCtx.SetVariable("actionType", action.Type)
	ae.execCtx.SetVariable("platform", action.TargetPlatform)
	ae.execCtx.SetVariable("reachedIndex", action.ReachedIndex)

	if action.ContentMessage != "" {
		ae.execCtx.SetVariable("messageText", action.ContentMessage)
		ae.execCtx.SetVariable("contentMessage", action.ContentMessage)
		ae.execCtx.SetVariable("commentText", action.ContentMessage)
		ae.execCtx.SetVariable("replyText", action.ContentMessage)
		ae.execCtx.SetVariable("text", action.ContentMessage)
	}
	if action.ContentSubject != "" {
		ae.execCtx.SetVariable("messageSubject", action.ContentSubject)
		ae.execCtx.SetVariable("contentSubject", action.ContentSubject)
	}
	if action.Keywords != "" {
		ae.execCtx.SetVariable("keyword", action.Keywords)
		ae.execCtx.SetVariable("keywords", action.Keywords)
		// URL-safe version for embedding in URL templates.
		ae.execCtx.SetVariable("keywordEncoded", url.QueryEscape(action.Keywords))
	}
	if action.ContentBlobURLs != "" {
		ae.execCtx.SetVariable("contentBlobUrls", action.ContentBlobURLs)
	}
	// Seed scheduling fields consumed by date-bounded actions (auto_reply_dms).
	if action.StartDate != "" {
		ae.execCtx.SetVariable("startDate", action.StartDate)
	}
	if action.EndDate != "" {
		ae.execCtx.SetVariable("endDate", action.EndDate)
	}
	if action.ExecutionInterval > 0 {
		ae.execCtx.SetVariable("pollInterval", action.ExecutionInterval)
	}
	// Seed all custom params as execution variables.
	for k, v := range action.Params {
		ae.execCtx.SetVariable(k, v)
	}
}

// validateRequiredInputs checks that every input declared as required by the
// action definition is present (non-empty) in the execution context. Required
// entries may be plain strings (legacy format) or objects with a "name" field.
//
// Inputs that declare "aliases" are filled from the first non-empty alias
// first (see resolveInputAliases), so a renamed input keeps accepting the
// config field older workflows set.
func (ae *ActionExecutor) validateRequiredInputs(def *ActionDef) error {
	if def.Inputs == nil {
		return nil
	}
	ae.resolveInputAliases(def.Inputs)
	var missing []string
	for _, raw := range def.Inputs.Required {
		name := requiredInputName(raw)
		if name == "" {
			continue
		}
		v, ok := ae.execCtx.GetVariable(name)
		if !ok || v == nil {
			missing = append(missing, name)
			continue
		}
		if isEmptyRequiredValue(v) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	for i, name := range missing {
		if canonical, ok := canonicalInputNames[name]; ok {
			missing[i] = canonical
		}
	}
	if len(missing) == 1 {
		return fmt.Errorf("missing required input '%s'", missing[0])
	}
	return fmt.Errorf("missing required inputs '%s'", strings.Join(missing, "', '"))
}

// canonicalInputNames maps a required input's legacy definition name to
// the name a node config sets it by, for error messages. The browser node
// fills selectedListItems from its targets list, so reporting
// "selectedListItems" sent readers looking for a field they never set.
var canonicalInputNames = map[string]string{"selectedListItems": "targets"}

// isEmptyRequiredValue reports whether a present value still amounts to
// "nothing was supplied" for a required input.
//
// Checking only for the empty string used to let two shapes through, both of
// which then ran an action that did nothing and reported success:
//
//   - An empty collection. `prompts: []` satisfies "present and not empty
//     string", and a loop over it runs zero times.
//   - The four characters "null". A node config of {{ json $json.prompts }}
//     renders exactly that when the field is absent, because that is what
//     json.Marshal(nil) produces — so the workflow that failed here passed a
//     required "prompts" of "null", iterated nothing, generated no images,
//     and finished green. A required input is never legitimately the literal
//     "null"; that string only ever means an expression found nothing.
func isEmptyRequiredValue(v interface{}) bool {
	switch val := v.(type) {
	case string:
		s := strings.TrimSpace(val)
		return s == "" || s == "null"
	case []interface{}:
		return len(val) == 0
	case []string:
		return len(val) == 0
	case []map[string]interface{}:
		return len(val) == 0
	case map[string]interface{}:
		return len(val) == 0
	}
	return false
}

// resolveInputAliases sets every declared input that is missing or empty
// from the first of its "aliases" that holds a value. A list value (the
// node's targets) fills a scalar input with its first entry, taking a
// map entry's url, href or username.
func (ae *ActionExecutor) resolveInputAliases(in *InputDef) {
	for _, raw := range append(append([]json.RawMessage{}, in.Required...), in.Optional...) {
		var obj struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Aliases []string `json:"aliases"`
		}
		if json.Unmarshal(raw, &obj) != nil || obj.Name == "" || len(obj.Aliases) == 0 {
			continue
		}
		if v, ok := ae.execCtx.GetVariable(obj.Name); ok && v != nil && !isEmptyRequiredValue(v) {
			continue
		}
		for _, alias := range obj.Aliases {
			v, ok := ae.execCtx.GetVariable(alias)
			if !ok || v == nil || isEmptyRequiredValue(v) {
				continue
			}
			if obj.Type != "list" && obj.Type != "array" {
				v = firstListEntry(v)
			}
			if v == nil || isEmptyRequiredValue(v) {
				continue
			}
			ae.execCtx.SetVariable(obj.Name, v)
			break
		}
	}
}

// firstListEntry reduces a list to its first entry (a map entry to its url,
// href or username); any other value is returned unchanged.
func firstListEntry(v interface{}) interface{} {
	var first interface{}
	switch l := v.(type) {
	case []interface{}:
		first = l[0]
	case []string:
		first = l[0]
	case []map[string]interface{}:
		first = l[0]
	default:
		return v
	}
	if m, ok := first.(map[string]interface{}); ok {
		for _, k := range []string{"url", "href", "username"} {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		return nil
	}
	return first
}

// requiredInputName extracts the input name from a required-input entry,
// accepting both the legacy string form ("messageText") and the object form
// ({"name": "messageText", ...}).
func requiredInputName(raw json.RawMessage) string {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return strings.TrimSpace(name)
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return strings.TrimSpace(obj.Name)
	}
	return ""
}

// ValidateActionInputs reports whether act supplies every input that
// platform/actionType declares required, plus any extras the caller has
// already resolved (selectedListItems and friends, keyed as the definition
// names them).
//
// It builds no browser session and runs no step, so a caller can check a
// misconfigured node before paying for a page: internal/nodes'
// BrowserNode.Execute otherwise opens a tab, then discovers the action had
// nothing to work with.
func ValidateActionInputs(platform, actionType string, act *StorageAction, extras map[string]interface{}) error {
	def, err := GetLoader().Load(platform, actionType)
	if err != nil {
		return fmt.Errorf("loading action definition: %w", err)
	}
	ae := NewActionExecutor(context.Background(), nil, nil, nil, nil, nil, zerolog.Nop())
	ae.seedVariables(act)
	for k, v := range extras {
		ae.SetVariable(k, v)
	}
	return ae.validateRequiredInputs(def)
}
