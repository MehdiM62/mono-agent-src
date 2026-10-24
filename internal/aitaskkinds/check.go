package aitaskkinds

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/monoes/mono-agent/internal/aitask"
)

// WorkflowExists says whether workflow id is in profile. The CLI sets it; nil
// means it cannot be checked, which refuses the result rather than trusting it.
var WorkflowExists func(ctx context.Context, profile, id string) (bool, error)

// CheckResult checks a result of kind against its schema and the caps on its
// strings and lists (aitask.Validate leaves maxLength and maxItems alone), and
// for workflow-generate that the workflow exists in the profile, so that the
// GUI never links to a missing id. It returns nil for a kind it has no checks
// for. A completion hook calls it with the task's profile.
func CheckResult(ctx context.Context, profile, kind string, raw []byte) error {
	k, err := aitask.Lookup(kind)
	if err != nil {
		return err
	}
	if _, mine := Descriptions[kind]; !mine {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("the result is not valid JSON: %w", err)
	}
	if err := aitask.Validate(k.Schema, v); err != nil {
		return err
	}
	if err := checkCaps("$", k.Schema, v); err != nil {
		return err
	}
	if kind != WorkflowGenerate {
		return nil
	}
	id, _ := v.(map[string]any)["workflowId"].(string)
	if WorkflowExists == nil {
		return fmt.Errorf("cannot check that workflow %q exists in this profile", id)
	}
	ok, err := WorkflowExists(ctx, profile, id)
	if err != nil {
		return fmt.Errorf("checking workflow %q: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("$.workflowId: no workflow %q in this profile: create it with the CLI first and report its real id", id)
	}
	return nil
}

func checkCaps(path string, schema map[string]any, v any) error {
	switch t := v.(type) {
	case string:
		if n, ok := schema["maxLength"].(float64); ok && float64(len([]rune(t))) > n {
			return fmt.Errorf("%s: must be at most %d characters", path, int(n))
		}
	case []any:
		if n, ok := schema["maxItems"].(float64); ok && float64(len(t)) > n {
			return fmt.Errorf("%s: must have at most %d items", path, int(n))
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range t {
				if err := checkCaps(fmt.Sprintf("%s[%d]", path, i), items, e); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		for k, e := range t {
			if sub, ok := props[k].(map[string]any); ok {
				if err := checkCaps(path+"."+k, sub, e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
