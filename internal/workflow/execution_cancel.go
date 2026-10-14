package workflow

import (
	"context"
	"errors"
	"fmt"
)

// routedToError reports whether outputs carry items on the "error" handle.
func routedToError(outputs []NodeOutput) bool {
	for _, out := range outputs {
		if out.Handle == "error" && len(out.Items) > 0 {
			return true
		}
	}
	return false
}

// cancelError is the error a run cancelled through ctx ends with: its cause
// when that is a cancel (a timeout, a refused account), the daemon stopping,
// or else ErrExecutionCancelled.
func cancelError(ctx context.Context) error {
	switch c := context.Cause(ctx); {
	case errors.Is(c, ErrEngineStopping):
		return fmt.Errorf("%w: %w", ErrExecutionCancelled, c)
	case errors.Is(c, ErrExecutionCancelled):
		return c
	}
	return ErrExecutionCancelled
}
