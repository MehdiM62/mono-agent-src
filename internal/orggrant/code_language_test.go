package orggrant

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// core.code's JavaScript only shapes items; its Python runs the system
// python3 and counts as outbound.
func TestCodeClassifiedByLanguage(t *testing.T) {
	for _, typ := range []string{"core.code", "code"} {
		for lang, want := range map[string]bool{"": false, "javascript": false, "python": true} {
			cfg := map[string]interface{}{"code": "return []"}
			if lang != "" {
				cfg["language"] = lang
			}
			if got := isOutboundNode(workflow.WorkflowNode{Type: typ, Config: cfg}); got != want {
				t.Errorf("%s language %q: outbound = %v, want %v", typ, lang, got, want)
			}
		}
	}
}
