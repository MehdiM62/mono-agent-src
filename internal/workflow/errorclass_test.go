package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/monomind"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{"nil", nil, ""},
		{"invalid config sentinel", fmt.Errorf("%w: agent.ask node requires \"prompt\"", ErrInvalidConfig), ClassInvalidConfig},
		{"unknown node type", fmt.Errorf("node x: %w", ErrNodeTypeUnknown), ClassInvalidConfig},
		{"not set up", monomind.MarkNotSetup(&monomind.ProtocolError{Code: monomind.ErrMissingBinary, Message: "claude not on PATH"}), ClassNotSetup},
		{"login required", fmt.Errorf("gate: %w", &account.LoginRequiredError{}), ClassAuth},
		{"rejected", fmt.Errorf("human_in_loop: %w", ErrRejected), ClassRejected},
		{"cancelled sentinel", ErrExecutionCancelled, ClassCancelled},
		{"context cancelled", fmt.Errorf("http: %w", context.Canceled), ClassCancelled},
		{"deadline", fmt.Errorf("http: %w", context.DeadlineExceeded), ClassTimeout},
		{"execution timeout", ErrExecutionTimeout, ClassTimeout},
		{"extension not connected", errors.New("Chrome extension not connected — no browser is launched as a fallback"), ClassBrowserUnavailable},
		{"selector", errors.New("FindFirst: none of the selectors matched"), ClassSelectorMissing},
		{"element", errors.New("mark element: element could not be located on the page"), ClassSelectorMissing},
		{"http 429", errors.New("http.request: unexpected status 429 Too Many Requests"), ClassRateLimited},
		{"rate limit words", errors.New("openrouter: rate limit exceeded"), ClassRateLimited},
		{"http 401", errors.New("slack: HTTP 401 unauthorized"), ClassAuth},
		{"http 403", errors.New("github: status code 403"), ClassAuth},
		{"http 502", errors.New("linear: status 502 Bad Gateway"), ClassUpstream5xx},
		{"http 503 words", errors.New("service unavailable (503)"), ClassUpstream5xx},
		{"timed out", errors.New("dial tcp: i/o timeout"), ClassTimeout},
		{"connection refused", errors.New(`Get "http://127.0.0.1:1/": dial tcp 127.0.0.1:1: connect: connection refused`), ClassNetwork},
		{"unknown host", errors.New("dial tcp: lookup nope.invalid: no such host"), ClassNetwork},
		{"connection reset", errors.New("read tcp 10.0.0.2:5000->10.0.0.3:443: read: connection reset by peer"), ClassNetwork},
		{"unreachable", errors.New("dial tcp 10.9.9.9:80: connect: network is unreachable"), ClassNetwork},
		{"other", errors.New("something odd happened"), ClassUnknown},
		{"permanent keeps inner class", Permanent(fmt.Errorf("%w: bad", ErrInvalidConfig)), ClassInvalidConfig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyError(c.err); got != c.want {
				t.Fatalf("ClassifyError(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

// A stored message (the only thing an execution row keeps) classifies the
// same way the error it came from did, for the classes a message can carry.
func TestClassifyMessage(t *testing.T) {
	cases := map[string]ErrorClass{
		"": "",
		"node n (N): " + ErrInvalidConfig.Error() + ": missing url": ClassInvalidConfig,
		"login_required: " + account.LoginRequiredMessage:           ClassAuth,
		"node h (Review): human_in_loop: " + ErrRejected.Error():    ClassRejected,
		"agent.ask: claude missing " + monomind.AgentNotSetupMarker: ClassNotSetup,
		"queue full":                        ClassUnknown,
		"workflow: execution was cancelled": ClassCancelled,
	}
	for msg, want := range cases {
		if got := ClassifyMessage(msg); got != want {
			t.Errorf("ClassifyMessage(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestErrorHintCoversEveryClass(t *testing.T) {
	for _, c := range AllErrorClasses {
		if ErrorHint(c) == "" {
			t.Errorf("class %q has no hint", c)
		}
	}
	if ErrorHint("") != "" {
		t.Errorf("empty class must have no hint")
	}
}
