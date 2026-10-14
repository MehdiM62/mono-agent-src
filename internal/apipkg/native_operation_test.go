package apipkg

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// TestExtendedNativeOperationSelects: on every native node a catalog
// package extends with resources, the native operation select shows only
// while no resource is picked, every native operation still validates
// without a resource, and a resource's operations validate with it.
func TestExtendedNativeOperationSelects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	Reload()
	t.Cleanup(Reload)
	n := 0
	for _, p := range packages() {
		if p.Extends == "" || len(p.Resources) == 0 {
			continue
		}
		n++
		s, err := workflow.LoadDefaultSchema(p.Extends)
		if err != nil {
			t.Fatalf("%s: %v", p.Extends, err)
		}
		var native *workflow.NodeSchemaField
		for i := range s.Fields {
			if f := &s.Fields[i]; f.Key == "operation" && f.DependsOn != nil && f.DependsOn.Key == "resource" &&
				len(f.DependsOn.Values) == 1 && f.DependsOn.Values[0] == "" {
				native = f
			}
		}
		if native == nil {
			t.Errorf("%s: no native operation select limited to an empty resource", p.Extends)
			continue
		}
		check := func(cfg map[string]any) {
			t.Helper()
			w := &workflow.Workflow{Name: "ops", Nodes: []workflow.WorkflowNode{{ID: "n", Type: p.Extends, Name: "N", Config: cfg}}}
			for _, is := range workflow.ValidateWorkflow(w, workflow.ValidateOptions{}).Issues {
				if is.Field == "operation" || is.Field == "resource" {
					t.Errorf("%s %v: %s %s (%s)", p.Extends, cfg, is.Code, is.Message, is.Hint)
				}
			}
		}
		for _, op := range native.Options {
			check(map[string]any{"operation": op})
		}
		for _, r := range p.Resources {
			for _, op := range p.opNames(r.Value) {
				check(map[string]any{"resource": r.Value, "operation": op})
			}
		}
	}
	if n < 21 {
		t.Errorf("checked %d extended natives, want at least 21", n)
	}
}

// TestOperationSelectPerResource: every package schema (and every native
// schema a package extends) has, for each resource value, exactly one
// operation select depending on it, and its default is one of its own
// options: a consumer picks the select whose depends_on matches the
// resource and reads that select's default.
func TestOperationSelectPerResource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	Reload()
	t.Cleanup(Reload)
	for _, p := range packages() {
		if len(p.Operations) == 0 || len(p.Resources) == 0 {
			continue
		}
		s, err := workflow.LoadDefaultSchema(p.TargetType())
		if err != nil {
			t.Fatalf("%s: %v", p.TargetType(), err)
		}
		var resources []string
		perResource := map[string]int{}
		for _, f := range s.Fields {
			switch {
			case f.Key == "resource" && f.Type == "select":
				resources = f.Options
			case f.Key == "operation":
				if f.DependsOn == nil || f.DependsOn.Key != "resource" || len(f.DependsOn.Values) != 1 {
					t.Errorf("%s: operation select %q does not depend on one resource: %+v", p.TargetType(), f.Label, f.DependsOn)
					continue
				}
				perResource[f.DependsOn.Values[0]]++
				if d, _ := f.Default.(string); !contains(f.Options, d) {
					t.Errorf("%s: %q default %v is not one of its options", p.TargetType(), f.Label, f.Default)
				}
			}
		}
		for _, r := range resources {
			if perResource[r] != 1 {
				t.Errorf("%s: resource %q has %d operation selects, want 1", p.TargetType(), r, perResource[r])
			}
		}
	}
}
