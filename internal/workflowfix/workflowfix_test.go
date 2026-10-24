package workflowfix

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

type fakeBackend struct {
	wf    *workflow.Workflow
	saved int
}

func (f *fakeBackend) Load(_ context.Context, profile, id string) (*workflow.Workflow, error) {
	if profile != "p1" || f.wf == nil || f.wf.ID != id {
		return nil, nil
	}
	cp := *f.wf
	cp.Nodes = append([]workflow.WorkflowNode(nil), f.wf.Nodes...)
	for i := range cp.Nodes { // the store hands out its own copy of the config
		c := map[string]any{}
		for k, v := range cp.Nodes[i].Config {
			c[k] = v
		}
		cp.Nodes[i].Config = c
	}
	return &cp, nil
}

func (f *fakeBackend) SaveNodes(_ context.Context, _, _ string, nodes []workflow.WorkflowNode) error {
	f.wf.Nodes = nodes
	f.saved++
	return nil
}

func rig(t *testing.T, active bool) *fakeBackend {
	t.Helper()
	b := &fakeBackend{wf: &workflow.Workflow{ID: "wf-1", Name: "Digest", IsActive: active,
		Nodes: []workflow.WorkflowNode{
			{ID: "t", Type: "trigger.manual", Name: "Start", Config: map[string]any{}},
			{ID: "set", Type: "core.set", Name: "Shape", Config: map[string]any{"assignments": "a=1"}},
			{ID: "http", Type: "http.request", Name: "Call", Config: map[string]any{"url": "https://example.test/a", "method": "POST"}},
		},
		Connections: []workflow.WorkflowConnection{
			{ID: "c1", SourceNodeID: "t", TargetNodeID: "set", SourceHandle: "main", TargetHandle: "main"},
			{ID: "c2", SourceNodeID: "set", TargetNodeID: "http", SourceHandle: "main", TargetHandle: "main"},
		}}}
	prevS, prevK := Store, NodeTypeKnown
	Store, NodeTypeKnown = b, nil
	t.Cleanup(func() { Store, NodeTypeKnown = prevS, prevK })
	return b
}

func result(kind string, edits ...map[string]any) []byte {
	fix := map[string]any{"kind": kind, "description": "d"}
	if edits != nil {
		fix["edits"] = edits
	}
	b, _ := json.Marshal(map[string]any{"summary": "s", "diagnosis": "d", "workflowId": "wf-1", "executionId": "ex-1", "fix": fix})
	return b
}

func edit(node string, config map[string]any) map[string]any {
	return map[string]any{"node": node, "config": config}
}

func TestPlanShowsBeforeAndAfterAndDeepMerges(t *testing.T) {
	rig(t, false)
	p, err := Plan(context.Background(), "p1", result(KindWorkflowEdit, edit("http", map[string]any{"url": "https://example.test/b"})))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Edits) != 1 || p.Edits[0].Path != "url" || p.Edits[0].Before != "https://example.test/a" ||
		p.Edits[0].After != "https://example.test/b" || p.Edits[0].NodeName != "Call" {
		t.Fatalf("%+v", p.Edits)
	}
	if got := deepMerge(map[string]any{"a": map[string]any{"x": 1, "y": 2}}, map[string]any{"a": map[string]any{"y": 3}}); got["a"].(map[string]any)["x"] != 1 || got["a"].(map[string]any)["y"] != 3 {
		t.Fatalf("not a deep merge: %v", got)
	}
}

func TestPlanRefusals(t *testing.T) {
	b := rig(t, false)
	ctx := context.Background()
	cases := map[string]struct {
		res  []byte
		code string
	}{
		"unknown node":     {result(KindWorkflowEdit, edit("nope", map[string]any{"url": "x"})), CodeUnknownNode},
		"advice":           {result(KindAdvice), CodeNoEdit},
		"secret key":       {result(KindWorkflowEdit, edit("http", map[string]any{"auth_token": "abc"})), CodeSecretKey},
		"nested secret":    {result(KindWorkflowEdit, edit("http", map[string]any{"headers": map[string]any{"Authorization": "x"}})), CodeSecretKey},
		"secret value":     {result(KindWorkflowEdit, edit("http", map[string]any{"url": "https://u:pw12345678@h.test/?x=1"})), CodeSecretKey},
		"no change":        {result(KindWorkflowEdit, edit("http", map[string]any{"method": "POST"})), CodeNoChange},
		"invalid config":   {result(KindWorkflowEdit, edit("http", map[string]any{"method": "TELEPORT"})), CodeInvalid},
		"unknown workflow": {[]byte(`{"workflowId":"other","fix":{"kind":"workflow-edit","edits":[{"node":"http","config":{"url":"x"}}]}}`), CodeNotFound},
	}
	for name, c := range cases {
		if _, err := Plan(ctx, "p1", c.res); !IsCode(err, c.code) {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
	// Another profile's workflow is not found.
	if _, err := Plan(ctx, "p2", result(KindWorkflowEdit, edit("http", map[string]any{"url": "x"}))); !IsCode(err, CodeNotFound) {
		t.Errorf("other profile: %v", err)
	}
	// Nothing was written by any of them.
	if b.saved != 0 {
		t.Errorf("a refused plan saved %d times", b.saved)
	}
	// A credential reference is a saved connection, not a secret.
	if _, err := Plan(ctx, "p1", result(KindWorkflowEdit, edit("http", map[string]any{"credential_id": "cred-1"}))); err != nil {
		t.Errorf("credential_id refused: %v", err)
	}
}

func TestPlanWarnings(t *testing.T) {
	rig(t, false)
	p, err := Plan(context.Background(), "p1", result(KindWorkflowEdit,
		edit("http", map[string]any{"url": "{{ $json.url }}", "credential_id": "cred-1"}),
		edit("set", map[string]any{"assignments": "a=2"})))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, w := range p.Warnings {
		have[w.Code] = true
	}
	for _, want := range []string{WarnNewOutbound, WarnCredentialKey, WarnExpressionAdd} {
		if !have[want] {
			t.Errorf("no %s warning: %+v", want, p.Warnings)
		}
	}
	// A read-only node's edit warns of nothing outbound.
	p, err = Plan(context.Background(), "p1", result(KindWorkflowEdit, edit("set", map[string]any{"assignments": "a=3"})))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range p.Warnings {
		if w.Code == WarnNewOutbound {
			t.Errorf("a core.set edit warned %+v", w)
		}
	}
}

func TestApplyStoresTheEditsOnceAndRefusesTheSecond(t *testing.T) {
	b := rig(t, false)
	res := result(KindWorkflowEdit, edit("http", map[string]any{"url": "https://example.test/b"}))
	n, err := Apply(context.Background(), "p1", res, Options{})
	if err != nil || n != 1 || b.saved != 1 || b.wf.Nodes[2].Config["url"] != "https://example.test/b" {
		t.Fatalf("n=%d err=%v saved=%d", n, err, b.saved)
	}
	if _, err := Apply(context.Background(), "p1", res, Options{}); !IsCode(err, CodeNoChange) || b.saved != 1 {
		t.Fatalf("second apply: %v (saved %d)", err, b.saved)
	}
}

func TestAnActiveWorkflowIsAWarningAndApplyNeedsAllowActive(t *testing.T) {
	b := rig(t, true)
	res := result(KindWorkflowEdit, edit("set", map[string]any{"assignments": "a=2"}))
	p, err := Plan(context.Background(), "p1", res)
	if err != nil || !p.Active || !strings.Contains(warnCodes(p), WarnWorkflowActive) {
		t.Fatalf("%v %+v", err, p)
	}
	if _, err := Apply(context.Background(), "p1", res, Options{}); !IsCode(err, CodeWorkflowActive) || b.saved != 0 {
		t.Fatalf("apply on an active workflow: %v (saved %d)", err, b.saved)
	}
	if n, err := Apply(context.Background(), "p1", res, Options{AllowActive: true}); err != nil || n != 1 || b.saved != 1 {
		t.Fatalf("--allow-active: %d %v", n, err)
	}
}

func TestPlanWithoutABackendRefuses(t *testing.T) {
	rig(t, false)
	Store = nil
	if _, err := Plan(context.Background(), "p1", result(KindWorkflowEdit, edit("set", map[string]any{"assignments": "a=2"}))); !IsCode(err, CodeNoBackend) {
		t.Fatal(err)
	}
}

func warnCodes(p Preview) string {
	var out []string
	for _, w := range p.Warnings {
		out = append(out, w.Code)
	}
	return strings.Join(out, ",")
}

func TestAWorkflowChangedAfterTheProposalIsStale(t *testing.T) {
	b := rig(t, false)
	res := result(KindWorkflowEdit, edit("set", map[string]any{"assignments": "a=2"}))
	done := time.Now()
	b.wf.UpdatedAt = done.Add(-time.Hour)
	if _, err := PlanSince(context.Background(), "p1", res, done); err != nil {
		t.Fatalf("an unchanged workflow is stale: %v", err)
	}
	b.wf.UpdatedAt = done.Add(time.Second)
	if _, err := PlanSince(context.Background(), "p1", res, done); !IsCode(err, CodeStale) {
		t.Fatalf("workflow edited after the proposal: %v", err)
	}
	// A node edited on its own (workflow node set) counts, whatever the workflow's own time says.
	b.wf.UpdatedAt = done.Add(-time.Hour)
	b.wf.Nodes[1].UpdatedAt = done.Add(time.Second)
	if _, err := Apply(context.Background(), "p1", res, Options{Since: done}); !IsCode(err, CodeStale) || b.saved != 0 {
		t.Fatalf("apply on a stale proposal: %v (saved %d)", err, b.saved)
	}
	// Without a time (the check at completion) there is nothing to compare.
	if _, err := Plan(context.Background(), "p1", res); err != nil {
		t.Fatal(err)
	}
}
