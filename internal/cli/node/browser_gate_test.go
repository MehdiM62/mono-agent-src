package node

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/action"
	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/nodes"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

// recordedPkg is the context of a package `record save` made.
type recordedPkg struct{ action.PackageContext }

func (recordedPkg) Trust() string            { return "recorded" }
func (recordedPkg) ID() string               { return "acme" }
func (recordedPkg) StartURL() string         { return "" }
func (recordedPkg) Domains() []string        { return nil }
func (recordedPkg) PermittedSteps() []string { return nil }

// recordedSource serves acme.press, a recorded package's action that clicks.
type recordedSource struct{}

func (recordedSource) Load(a, t string) ([]byte, error) {
	if a != "acme" || t != "press" {
		return nil, fmt.Errorf("not found %s/%s", a, t)
	}
	return []byte(`{"actionType":"press","sideEffects":"read","steps":[{"id":"go","type":"click","selector":"#go"}]}`), nil
}
func (recordedSource) List() ([]string, error) { return []string{"acme/press"}, nil }
func (recordedSource) Package(a string) action.PackageContext {
	if a == "acme" {
		return recordedPkg{}
	}
	return nil
}

// An agent's `node run` of a recorded package's browser action is refused
// (exit 3, operator_only) before a browser is connected, or launched: such
// an action runs in the user's logged-in browser as its files say, and an
// agent can write those files (`record save`, or by hand). The user's run
// goes on to the browser.
func TestNodeRunOfAnAgentRunsNoRecordedBrowserAction(t *testing.T) {
	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
	action.SetDefSource(recordedSource{})
	t.Cleanup(func() { action.SetDefSource(nil) })
	d := testDeps(nodeVaultTestDB(t), "default", false)
	d.BuildRegistry = func(*sql.DB) *workflow.NodeTypeRegistry {
		r := workflow.NewNodeTypeRegistry()
		r.Register("acme.press", func() workflow.NodeExecutor { return nodes.NewBrowserNode("acme", "press") })
		return r
	}
	connects := 0
	d.ConnectBridge = func(string, zerolog.Logger) (browserpkg.ExtensionBridge, error) {
		connects++
		return nil, errors.New("no browser in tests")
	}
	raw, _ := json.Marshal(map[string]interface{}{"config": map[string]interface{}{}})
	for _, marker := range []string{"CLAUDECODE", "AI_AGENT"} {
		t.Setenv(marker, "1")
		_, err := runNodeSubcmdStdin(t, d, string(raw), "acme.press")
		var ce *clicore.Error
		var fields interface{ JSONErrorFields() map[string]any }
		if !errors.As(err, &ce) || ce.Code != 3 || !errors.As(err, &fields) || fields.JSONErrorFields()["code"] != "operator_only" {
			t.Errorf("%s: node run acme.press: %v, want exit 3 operator_only", marker, err)
		}
		if connects != 0 {
			t.Fatalf("%s: the browser was connected %d time(s)", marker, connects)
		}
		t.Setenv(marker, "")
	}
	if _, err := runNodeSubcmdStdin(t, d, string(raw), "acme.press"); err == nil || connects != 1 {
		t.Fatalf("the user's node run: %v (connects %d), want it to reach the browser", err, connects)
	}
}
