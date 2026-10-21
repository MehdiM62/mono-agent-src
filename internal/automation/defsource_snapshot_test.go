package automation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/action"
)

// One load yields the definition and the trust decision from the same
// verified snapshot: tampering with a built-in's action file and restoring it
// must neither hand the tampered definition to a built-in context, nor leave
// the tampered definition cached once the files are good again (#900).
func TestLoaderBindsDefinitionAndTrustToOneSnapshot(t *testing.T) {
	r := seededOfficial(t)
	action.SetDefSource(r.DefSource())
	t.Cleanup(func() { action.SetDefSource(nil) })

	f := filepath.Join(hackernewsDir(t, r), "actions", "get_post_metrics.json")
	orig, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	trustOf := func() string {
		def, err := action.GetLoader().Load("hackernews", "get_post_metrics")
		if err != nil {
			t.Fatal(err)
		}
		p := def.Package()
		if p == nil {
			t.Fatal("definition carries no package context")
		}
		return action.PackageTrust(p)
	}
	var m map[string]any
	if err := json.Unmarshal(orig, &m); err != nil {
		t.Fatal(err)
	}
	m["description"] = "TAMPERED"
	tampered, _ := json.Marshal(m)
	if err := os.WriteFile(f, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := trustOf(); got == "builtin" {
		t.Fatal("a tampered built-in's definition runs with built-in trust")
	}
	// Reload: a definition read from the tampered file must not be cached
	// for use after the file is restored.
	if err := os.WriteFile(f, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := trustOf(); got != "builtin" {
		t.Fatalf("restored trust = %q", got)
	}
	def, err := action.GetLoader().Load("hackernews", "get_post_metrics")
	if err != nil {
		t.Fatal(err)
	}
	if got := action.PackageTrust(def.Package()); got != "builtin" {
		t.Fatalf("restored trust = %q", got)
	}
	if def.Description == "TAMPERED" {
		t.Fatal("the tampered definition stayed cached after the file was restored")
	}
}
