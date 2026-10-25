package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func publicationsEnv(info *WorkflowPublicationsInfo, err error) *Env {
	return &Env{WorkflowPublications: func(context.Context) (*WorkflowPublicationsInfo, error) { return info, err }}
}

func TestWorkflowPublicationsCheck(t *testing.T) {
	stale := OpenWorkflowPublication{WorkflowID: "wf-1", Kind: "save", Origin: "apply-fix", State: "pending", Age: time.Minute, OwnerGone: true}
	cases := []struct {
		name    string
		env     *Env
		status  Status
		summary string
		fix     string
	}{
		{"no hook", &Env{}, StatusSkip, "not available", ""},
		{"clean", publicationsEnv(&WorkflowPublicationsInfo{}, nil), StatusOK, "agree", ""},
		{"unreadable hook", publicationsEnv(nil, errors.New("boom")), StatusWarn, "cannot read", ""},
		{"abandoned change", publicationsEnv(&WorkflowPublicationsInfo{Open: []OpenWorkflowPublication{stale}}, nil), StatusWarn, "1 unfinished", FixWorkflowPublicationsRecover},
		{"change in progress", publicationsEnv(&WorkflowPublicationsInfo{Open: []OpenWorkflowPublication{{WorkflowID: "wf-2", Kind: "save", State: "pending"}}}, nil), StatusInfo, "in progress", ""},
		{"needs a choice", publicationsEnv(&WorkflowPublicationsInfo{Open: []OpenWorkflowPublication{{WorkflowID: "wf-3", Kind: "save", State: "unresolved", Note: "copy kept"}}}, nil), StatusWarn, "needing a choice", ""},
		{"unreadable file", publicationsEnv(&WorkflowPublicationsInfo{Unreadable: []string{"wf-4"}}, nil), StatusWarn, "unreadable file", ""},
		{"waiting import", publicationsEnv(&WorkflowPublicationsInfo{Pending: []string{"wf-5"}}, nil), StatusInfo, "waiting", ""},
	}
	for _, c := range cases {
		res := checkWorkflowPublications(context.Background(), c.env)
		if res.Status != c.status || !strings.Contains(res.Summary, c.summary) || res.FixID != c.fix {
			t.Errorf("%s: %s %q fix %q, want %s containing %q fix %q", c.name, res.Status, res.Summary, res.FixID, c.status, c.summary, c.fix)
		}
	}
}

func TestWorkflowPublicationsFixRunsTheRecovery(t *testing.T) {
	called := 0
	env := &Env{RecoverWorkflowPublications: func(context.Context) (int, error) { called++; return 2, nil }}
	if err := fixWorkflowPublications(context.Background(), env, func(string) {}); err != nil || called != 1 {
		t.Fatalf("called %d err %v", called, err)
	}
	if err := fixWorkflowPublications(context.Background(), &Env{}, func(string) {}); err == nil {
		t.Fatal("a fix with no recovery available must say so")
	}
}

func TestWorkflowPublicationsRowIsRegistered(t *testing.T) {
	found := false
	for _, c := range Default().Checks() {
		if c.ID == CheckWorkflowPublications {
			found = len(c.DependsOn) == 1 && c.DependsOn[0] == CheckDB
		}
	}
	if !found {
		t.Fatal("the row is missing or does not wait for the database")
	}
	if _, ok := Default().Fix(FixWorkflowPublicationsRecover); !ok {
		t.Fatal("the fix is not registered")
	}
}
