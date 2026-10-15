package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// A cancel the daemon makes because the account locked for a reason other than
// a refusal (expired, logged out, never signed in) records login_required and
// the reason, not the refusal's wording.
func TestCancelledMessageOfALockedAccount(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(fmt.Errorf("%w (%s)", errAccountLocked, "expired"))
	got := cancelledMessage(ctx, ErrExecutionCancelled)
	if !strings.HasPrefix(got, "login_required: the monoes.me account is locked (expired)") {
		t.Fatalf("cancelledMessage = %q, want login_required with the lock reason", got)
	}
	if strings.Contains(got, "refused") {
		t.Fatalf("cancelledMessage = %q names a refusal", got)
	}

	refused, cancelRefused := context.WithCancelCause(context.Background())
	cancelRefused(errAccountRefused)
	if got := cancelledMessage(refused, ErrExecutionCancelled); !strings.Contains(got, "refused this account") {
		t.Fatalf("a refusal's cancel = %q, want the refusal wording kept", got)
	}
}

// CancelRunningLocked with nothing running asks nothing and reports zero.
func TestCancelRunningLockedWithNothingRunning(t *testing.T) {
	e := &WorkflowEngine{queue: NewExecutionQueue(1, 1, func(context.Context, ExecutionRequest) {}, zerolog.Nop())}
	if n := e.CancelRunningLocked("not_logged_in"); n != 0 {
		t.Fatalf("CancelRunningLocked = %d, want 0", n)
	}
}
