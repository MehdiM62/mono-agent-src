package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/nodes"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

type gateRecordedPkg struct{ action.PackageContext }

func (gateRecordedPkg) Trust() string            { return "recorded" }
func (gateRecordedPkg) ID() string               { return "acme" }
func (gateRecordedPkg) StartURL() string         { return "" }
func (gateRecordedPkg) Domains() []string        { return nil }
func (gateRecordedPkg) PermittedSteps() []string { return nil }

type gateRecordedSource struct{}

func (gateRecordedSource) Load(a, t string) ([]byte, error) {
	if a != "acme" || t != "press" {
		return nil, fmt.Errorf("not found %s/%s", a, t)
	}
	return []byte(`{"actionType":"press","sideEffects":"read","steps":[{"id":"go","type":"click","selector":"#go"}]}`), nil
}
func (gateRecordedSource) List() ([]string, error) { return []string{"acme/press"}, nil }
func (gateRecordedSource) Package(a string) action.PackageContext {
	if a == "acme" {
		return gateRecordedPkg{}
	}
	return nil
}

type tabCounter struct{ calls int }

func (p *tabCounter) GetPage(context.Context, string, string) (browser.PageInterface, error) {
	p.calls++
	return nil, fmt.Errorf("no browser in tests")
}

// node_run of a recorded package's browser action is refused (operator_only)
// before a tab opens, whether the server runs under an agent's marker or not:
// what node_run runs, an AI wrote. The action would run in the user's
// logged-in browser as its files say.
func TestNodeRunToolRunsNoRecordedBrowserAction(t *testing.T) {
	action.SetDefSource(gateRecordedSource{})
	tabs := &tabCounter{}
	nodes.SetGlobalSessionProvider(tabs)
	t.Cleanup(func() { action.SetDefSource(nil); nodes.SetGlobalSessionProvider(nil) })
	for _, marker := range []string{"CLAUDECODE", ""} {
		for _, k := range operator.MarkerEnv() {
			t.Setenv(k, "")
		}
		if marker != "" {
			t.Setenv(marker, "1")
		}
		s := newTestServerAllowMutations(t, true)
		rt, err := s.runtime()
		if err != nil {
			t.Fatal(err)
		}
		r := workflow.NewNodeTypeRegistry()
		r.Register("acme.press", func() workflow.NodeExecutor { return nodes.NewBrowserNode("acme", "press") })
		rt.registry = r
		_, err = callNamed(t, s, "node_run", map[string]interface{}{"type": "acme.press", "config": map[string]interface{}{}})
		if err == nil || !strings.Contains(err.Error(), "operator_only") || tabs.calls != 0 {
			t.Fatalf("marker %q: node_run acme.press: %v (tabs %d), want operator_only before a tab", marker, err, tabs.calls)
		}
	}
}
