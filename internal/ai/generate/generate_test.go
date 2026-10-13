package generate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/workflow"
)

const validDoc = `{"name":"Fetch","nodes":[
 {"id":"n1","type":"trigger.manual","name":"Start","position":{"x":100,"y":100},"config":{}},
 {"id":"n2","type":"http.request","name":"Fetch","position":{"x":350,"y":100},"config":{"url":"https://example.com"}}],
 "connections":[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]}`

// noTrigger fails activation validation (no trigger node).
const noTrigger = `{"name":"Fetch","nodes":[
 {"id":"n2","type":"http.request","name":"Fetch","position":{"x":350,"y":100},"config":{}}],"connections":[]}`

const inventedType = `{"name":"Fetch","nodes":[
 {"id":"n1","type":"trigger.manual","name":"Start","config":{}},
 {"id":"n2","type":"magic.do_it","name":"Magic","config":{}}],
 "connections":[{"id":"c1","source":"n1","target":"n2"}]}`

// fakeExec answers with answers in turn and records the prompts it got.
type fakeExec struct {
	answers []string
	prompts []string
	system  string
}

func (f *fakeExec) exec(_ context.Context, prompt, system string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	f.system = system
	i := len(f.prompts) - 1
	if i >= len(f.answers) {
		i = len(f.answers) - 1
	}
	return f.answers[i], nil
}

func known(t string) bool { return t == "http.request" || t == "core.if" }

func request(f *fakeExec) Request {
	return Request{
		Description: "fetch example.com",
		MaxAttempts: 3,
		Catalog:     BuildCatalog(append([]string{"http.request", "core.if"}, TriggerTypes()...), nil, nil),
		Exec:        f.exec,
		Validate:    Validator(known),
	}
}

func TestGenerateRetriesAfterInvalidJSON(t *testing.T) {
	f := &fakeExec{answers: []string{"Sure! Here is the workflow: {\"name\": ", "```json\n" + validDoc + "\n```"}}
	res, err := Generate(context.Background(), request(f))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Attempts != 2 || len(res.History) != 2 || len(res.History[0].Errors) == 0 {
		t.Fatalf("attempts = %d, history = %+v", res.Attempts, res.History)
	}
	if !strings.Contains(f.prompts[1], "no complete JSON object") {
		t.Errorf("retry prompt does not carry the parse problem:\n%s", f.prompts[1])
	}
	if len(res.Workflow.Nodes) != 2 || res.Workflow.IsActive {
		t.Errorf("workflow = %+v", res.Workflow)
	}
	if res.Workflow.Description != "fetch example.com" {
		t.Errorf("description = %q, want the request's", res.Workflow.Description)
	}
}

func TestGenerateFeedsValidationErrorsBack(t *testing.T) {
	f := &fakeExec{answers: []string{noTrigger, inventedType, validDoc}}
	res, err := Generate(context.Background(), request(f))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", res.Attempts)
	}
	if !strings.Contains(f.prompts[1], "trigger") || !strings.Contains(f.prompts[1], `"http.request"`) {
		t.Errorf("second prompt lacks the trigger error or the previous answer:\n%s", f.prompts[1])
	}
	if !strings.Contains(f.prompts[2], `unknown node type "magic.do_it"`) {
		t.Errorf("third prompt lacks the unknown-type error:\n%s", f.prompts[2])
	}
	// Empty handles were normalized to main before validation.
	if res.Workflow.Connections[0].SourceHandle != "main" {
		t.Errorf("handle = %q", res.Workflow.Connections[0].SourceHandle)
	}
}

func TestGenerateGivesUpAfterMaxAttempts(t *testing.T) {
	f := &fakeExec{answers: []string{noTrigger}}
	_, err := Generate(context.Background(), request(f))
	var ae *AttemptsError
	if !errors.As(err, &ae) || !errors.Is(err, ErrNotValid) {
		t.Fatalf("err = %v, want an AttemptsError", err)
	}
	if len(f.prompts) != 3 || len(ae.History) != 3 {
		t.Fatalf("turns = %d, history = %d, want 3", len(f.prompts), len(ae.History))
	}
	if !strings.Contains(err.Error(), "3 attempt(s)") || !strings.Contains(err.Error(), "trigger") {
		t.Errorf("error = %q", err)
	}
}

func TestGenerateStopsOnExecError(t *testing.T) {
	calls := 0
	req := request(&fakeExec{})
	req.Exec = func(context.Context, string, string) (string, error) {
		calls++
		return "", errors.New("not logged in")
	}
	if _, err := Generate(context.Background(), req); err == nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestGenerateNameOverride(t *testing.T) {
	f := &fakeExec{answers: []string{validDoc}}
	req := request(f)
	req.Name = "Mine"
	res, err := Generate(context.Background(), req)
	if err != nil || res.Workflow.Name != "Mine" || res.Document.Name != "Mine" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if res.Document.Nodes[1].Type != "http.request" || res.Document.Connections[0].Source != "n1" {
		t.Errorf("document = %+v", res.Document)
	}
}

func TestExtractJSON(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"text before ```json\n{\"a\":\"}\"}\n``` after", `{"a":"}"}`},
		{`see {not json} then {"b":{"c":2}} end`, `{"b":{"c":2}}`},
	} {
		got, err := ExtractJSON(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ExtractJSON(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ExtractJSON("no json here"); err == nil {
		t.Error("want an error for text without JSON")
	}
}

func TestSystemPromptListsCatalog(t *testing.T) {
	cat := BuildCatalog([]string{"agent.ask", "ai.chat", "trigger.schedule"}, func(t string) string {
		if t == "agent.ask" {
			return "Ask a local agent"
		}
		return ""
	}, nil)
	p := SystemPrompt(cat)
	if !strings.Contains(p, "agent.ask — Ask a local agent. req: runtime(text), prompt(textarea)") {
		t.Errorf("agent.ask line missing:\n%s", p)
	}
	if !strings.Contains(p, "trigger.schedule — Schedule") {
		t.Errorf("trigger line missing:\n%s", p)
	}
	if strings.Contains(p, "ai.chat —") {
		t.Error("deprecated ai.chat is offered")
	}
}

func TestDraftGivesFreshIDs(t *testing.T) {
	wf, err := Parse(validDoc)
	if err != nil {
		t.Fatal(err)
	}
	wf.ID = "existing-workflow"
	d := Draft(wf)
	if d.ID != "" || d.IsActive {
		t.Fatalf("draft id/active = %q/%v", d.ID, d.IsActive)
	}
	if d.Nodes[0].ID == "n1" || d.Connections[0].SourceNodeID != d.Nodes[0].ID || d.Connections[0].TargetNodeID != d.Nodes[1].ID {
		t.Errorf("ids not remapped consistently: %+v / %+v", d.Nodes, d.Connections)
	}
	if err := workflow.ValidateForActivation(&d); err != nil {
		t.Errorf("draft no longer validates: %v", err)
	}
}

func TestPickRuntime(t *testing.T) {
	now := time.Now()
	yes := true
	scan := &monomind.ScanResult{Agents: []monomind.ScanEntry{
		{ID: "codex", Installed: true, ExecutionSupported: &yes},
		{ID: "claude", Installed: true},
	}}
	results := []agentroster.Result{
		{Runtime: "claude", Model: "old", Status: agentroster.StatusOK, ValidatedAt: now.Add(-30 * 24 * time.Hour)},
		{Runtime: "codex", Model: "gpt-x", Status: agentroster.StatusOK, ValidatedAt: now},
	}
	rt, model, err := PickRuntime(results, scan)
	if err != nil || rt != "codex" || model != "gpt-x" {
		t.Errorf("ready pick = %s/%s, %v", rt, model, err)
	}
	rt, model, err = PickRuntime(results[:1], scan)
	if err != nil || rt != "claude" || model != "old" {
		t.Errorf("stale pick = %s/%s, %v", rt, model, err)
	}
	rt, _, err = PickRuntime(nil, scan)
	if err != nil || rt != "codex" {
		t.Errorf("installed pick = %s, %v", rt, err)
	}
	if _, _, err := PickRuntime(nil, &monomind.ScanResult{}); !errors.Is(err, ErrNoRuntime) || !strings.Contains(err.Error(), "agent install") {
		t.Errorf("none: %v", err)
	}
}
