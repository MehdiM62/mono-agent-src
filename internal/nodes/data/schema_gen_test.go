package data

import (
	"bytes"
	"os"
	"testing"

	"github.com/monoes/mono-agent/internal/tools/schemagen"
)

// newSchemas lists node types whose schema JSON is generated from a
// companion struct in this package but that are not (yet) in
// schemagen.Manifest. Files add their entries from init(). Until the
// manifest picks them up, this test is their drift check: it fails when the
// committed JSON differs from what the struct tags generate. Regenerate
// with MONO_SCHEMAGEN_WRITE=1 go test ./internal/nodes/data -run TestNewSchemasGenerated
var newSchemas []schemagen.ManifestEntry

func TestNewSchemasGenerated(t *testing.T) {
	root, err := schemagen.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	write := os.Getenv("MONO_SCHEMAGEN_WRITE") == "1"
	for _, e := range newSchemas {
		want, err := schemagen.RenderJSON(root, e)
		if err != nil {
			t.Errorf("%s: %v", e.NodeType, err)
			continue
		}
		path := schemagen.SchemaPath(root, e)
		if write {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", e.NodeType, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: %s is out of date; regenerate with MONO_SCHEMAGEN_WRITE=1", e.NodeType, path)
		}
	}
}
