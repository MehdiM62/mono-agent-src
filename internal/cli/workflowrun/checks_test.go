package workflowrun

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A template whose node types are not installed fails before anything is
// created, naming the types and the automation that provides them.
func TestCheckTemplateNodeTypesNamesTheAutomation(t *testing.T) {
	t.Parallel()
	tmpl, ok := workflow.GetTemplate("gemimg")
	if !ok {
		t.Skip("gemimg template not bundled")
	}
	noGemini := func(typ string) bool { return !strings.HasPrefix(typ, "gemini.") }
	err := CheckTemplateNodeTypes("gemimg", tmpl, noGemini)
	if err == nil {
		t.Fatal("want an error for missing gemini.* types")
	}
	for _, want := range []string{"gemini.generate_image", "monoagentcli library install automation gemini"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if exitCode(err) != 3 || !errors.Is(err, workflow.ErrNodeTypeUnknown) {
		t.Errorf("exit %d, want 3 via ErrNodeTypeUnknown", exitCode(err))
	}
	if err := CheckTemplateNodeTypes("gemimg", tmpl, func(string) bool { return true }); err != nil {
		t.Errorf("all types installed: %v", err)
	}
}

func TestMissingTemplateNodeTypesSkipsTriggers(t *testing.T) {
	t.Parallel()
	tmpl := workflow.WorkflowFile{Nodes: []workflow.WorkflowFileNode{
		{Type: "trigger.manual"}, {Type: "x.post"}, {Type: "x.post"}, {Type: "core.if"},
	}}
	got := missingTemplateNodeTypes(tmpl, func(t string) bool { return t == "core.if" })
	if fmt.Sprint(got) != "[x.post]" {
		t.Fatalf("missing = %v, want [x.post]", got)
	}
}

func TestAutomationInstallHint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		types []string
		want  string
	}{
		{[]string{"gemini.generate_image"}, `this needs the "gemini" web automation: monoagentcli library install automation gemini`},
		{[]string{"x.post", "linkedin.like", "x.like"}, "monoagentcli library install automation linkedin; monoagentcli library install automation x"},
		{[]string{"core.nope"}, "monoagentcli library list --kind automation"},
	} {
		if got := automationInstallHint(tc.types); !strings.Contains(got, tc.want) {
			t.Errorf("%v: hint %q lacks %q", tc.types, got, tc.want)
		}
	}
}

func TestWithNodeTypeHint(t *testing.T) {
	t.Parallel()
	err := withNodeTypeHint(errors.New("execution e1 failed: workflow: unknown node type: gemini.generate_image"))
	if !strings.HasSuffix(err.Error(), "— this needs the \"gemini\" web automation: monoagentcli library install automation gemini") {
		t.Errorf("err = %v", err)
	}
	plain := errors.New("execution e1 failed: boom")
	if withNodeTypeHint(plain) != plain || withNodeTypeHint(nil) != nil {
		t.Error("an unrelated error must be returned as it is")
	}
}

func TestCheckTemplateInputs(t *testing.T) {
	t.Parallel()
	err := checkTemplateInputs([]string{"keywords", "location"}, map[string]interface{}{})
	want := `missing input: keywords, location — run with --input '{"keywords":"...","location":"..."}'`
	if err == nil || err.Error() != want || exitCode(err) != 3 {
		t.Fatalf("err = %v (exit %d), want %q (exit 3)", err, exitCode(err), want)
	}
	if err := checkTemplateInputs([]string{"keywords", "location"}, map[string]interface{}{"keywords": "go"}); err != nil {
		t.Errorf("given input: %v", err)
	}
	if err := checkTemplateInputs(nil, nil); err != nil {
		t.Errorf("template without inputs: %v", err)
	}
}

func TestNothingSucceeded(t *testing.T) {
	t.Parallel()
	failed := executionSummaryJSON{ExecutionID: "e1", Status: "SUCCESS_WITH_ERRORS", Error: "3 node(s) failed", Nodes: []executionNodeJSON{
		{Type: "trigger.manual", Status: "SUCCESS"}, {Type: "discovery.search_jobs", Status: "FAILED"},
	}}
	if err := nothingSucceeded(failed); err == nil || exitCode(err) != 1 || !strings.Contains(err.Error(), "every step failed") {
		t.Errorf("every non-trigger node failed: err = %v", err)
	}
	partial := failed
	partial.Nodes = append(append([]executionNodeJSON{}, failed.Nodes...), executionNodeJSON{Type: "core.set", Status: "SUCCESS"})
	if err := nothingSucceeded(partial); err != nil {
		t.Errorf("partial success: %v", err)
	}
	if err := nothingSucceeded(executionSummaryJSON{Status: "SUCCESS"}); err != nil {
		t.Errorf("success: %v", err)
	}
}
