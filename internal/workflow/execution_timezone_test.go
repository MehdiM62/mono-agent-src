package workflow

import (
	"strings"
	"testing"
)

func TestNowUsesWorkflowTimezone(t *testing.T) {
	t.Setenv(TimezoneEnv, "")
	e := NewExpressionEngine()
	exec := &WorkflowExecution{ID: "e"}
	eval := func(wf *Workflow, tmpl string) string {
		t.Helper()
		got, err := e.EvaluateString(tmpl, buildExpressionContext(nil, nil, wf, exec))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := eval(&Workflow{}, `{{ $now }}`); !strings.HasSuffix(got, "Z") {
		t.Fatalf("default $now = %q, want UTC", got)
	}
	tokyo := &Workflow{Settings: map[string]interface{}{TimezoneSetting: "Asia/Tokyo"}}
	if got := eval(tokyo, `{{ $now }}`); !strings.HasSuffix(got, "+09:00") {
		t.Fatalf("Tokyo $now = %q", got)
	}
	if got := eval(tokyo, `{{ $today }}`); !strings.HasSuffix(got, "T00:00:00+09:00") {
		t.Fatalf("Tokyo $today = %q", got)
	}

	t.Setenv(TimezoneEnv, "America/New_York")
	if got := eval(&Workflow{}, `{{ $now.Location }}`); got != "America/New_York" {
		t.Fatalf("env default zone = %q", got)
	}
	if got := eval(tokyo, `{{ $now.Location }}`); got != "Asia/Tokyo" {
		t.Fatalf("setting should win over env, got %q", got)
	}
}

func TestValidateWarnsUnknownTimezone(t *testing.T) {
	wf := &Workflow{Settings: map[string]interface{}{TimezoneSetting: "Mars/Olympus"}, Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
	}}
	r := ValidateWorkflow(wf, ValidateOptions{})
	if len(r.Warnings) == 0 || !strings.Contains(strings.Join(r.Warnings, "\n"), "Mars/Olympus") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}
