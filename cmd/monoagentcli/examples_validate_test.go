package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExampleWorkflowsValidate keeps every top-level examples/*.json importable.
// The examples README promises each node type has a schema in
// internal/workflow/schemas and each config key is one of that schema's fields;
// `workflow validate --file` checks the graph shape but not those, so check
// them here too.
func TestExampleWorkflowsValidate(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no example workflows found")
	}
	schemaDir := filepath.Join("..", "..", "internal", "workflow", "schemas")
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			out, err := runValidateCmd(t, false, "--file", f)
			if err != nil {
				t.Fatalf("validate %s: %v\n%s", f, err, out)
			}
			if !strings.Contains(out, "Workflow is valid.") {
				t.Fatalf("validate %s: unexpected output %q", f, out)
			}

			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var wf struct {
				Nodes []struct {
					ID     string                 `json:"id"`
					Type   string                 `json:"type"`
					Config map[string]interface{} `json:"config"`
				} `json:"nodes"`
			}
			if err := json.Unmarshal(raw, &wf); err != nil {
				t.Fatal(err)
			}
			for _, n := range wf.Nodes {
				sraw, err := os.ReadFile(filepath.Join(schemaDir, n.Type+".json"))
				if err != nil {
					t.Errorf("node %s: no schema for type %q", n.ID, n.Type)
					continue
				}
				var schema struct {
					Fields []struct {
						Key string `json:"key"`
					} `json:"fields"`
				}
				if err := json.Unmarshal(sraw, &schema); err != nil {
					t.Fatalf("schema %s: %v", n.Type, err)
				}
				known := map[string]bool{}
				for _, fld := range schema.Fields {
					known[fld.Key] = true
				}
				for k := range n.Config {
					if !known[k] {
						t.Errorf("node %s (%s): config key %q is not a schema field", n.ID, n.Type, k)
					}
				}
			}
		})
	}
}
