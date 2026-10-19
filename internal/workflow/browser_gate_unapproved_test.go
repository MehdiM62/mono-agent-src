package workflow_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/nodes"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

type unapprovedRecordedPkg struct{ action.PackageContext }

func (unapprovedRecordedPkg) Trust() string            { return "recorded" }
func (unapprovedRecordedPkg) ID() string               { return "acme" }
func (unapprovedRecordedPkg) StartURL() string         { return "" }
func (unapprovedRecordedPkg) Domains() []string        { return nil }
func (unapprovedRecordedPkg) PermittedSteps() []string { return nil }

type unapprovedRecordedSource struct{}

func (unapprovedRecordedSource) Load(a, t string) ([]byte, error) {
	if a != "acme" || t != "press" {
		return nil, fmt.Errorf("not found %s/%s", a, t)
	}
	return []byte(`{"actionType":"press","sideEffects":"read","steps":[{"id":"go","type":"click","selector":"#go"}]}`), nil
}
func (unapprovedRecordedSource) List() ([]string, error) { return []string{"acme/press"}, nil }
func (unapprovedRecordedSource) Package(a string) action.PackageContext {
	if a == "acme" {
		return unapprovedRecordedPkg{}
	}
	return nil
}

type stepTabs struct{ calls int }

func (p *stepTabs) GetPage(context.Context, string, string) (browser.PageInterface, error) {
	p.calls++
	return nil, fmt.Errorf("no browser in tests")
}

// A step of a workflow the user hasn't approved that runs a recorded
// package's browser action fails before a tab opens, whoever runs the
// workflow (no marker here: the run's own approval decides); the user's
// approved run reaches the browser.
func TestAnUnapprovedWorkflowStepRunsNoRecordedBrowserAction(t *testing.T) {
	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
	action.SetDefSource(unapprovedRecordedSource{})
	tabs := &stepTabs{}
	nodes.SetGlobalSessionProvider(tabs)
	t.Cleanup(func() { action.SetDefSource(nil); nodes.SetGlobalSessionProvider(nil) })
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("acme.press", func() workflow.NodeExecutor { return nodes.NewBrowserNode("acme", "press") })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "p", Type: "acme.press", Name: "Press"},
	}, Connections: []workflow.WorkflowConnection{conn("t", "main", "p")}}
	run := func(ctx context.Context) error {
		t.Helper()
		dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
		if err != nil {
			t.Fatal(err)
		}
		exec := &workflow.WorkflowExecution{ID: "e", WorkflowID: wf.ID, TriggerData: map[string]interface{}{}}
		return workflow.RunExecution(ctx, exec, wf, dag, reg, &recStore{}, nil, workflow.NewExpressionEngine(), zerolog.Nop())
	}
	if err := run(workflow.WithAIAuthored(context.Background())); err == nil || !strings.Contains(err.Error(), "only the user runs") || tabs.calls != 0 {
		t.Fatalf("the unapproved step: %v, tabs %d; want it refused before a tab", err, tabs.calls)
	}
	run(context.Background())
	if tabs.calls != 1 {
		t.Fatalf("the user's approved run opened %d tab(s), want 1", tabs.calls)
	}
}
