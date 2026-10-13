package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"time"
)

// executeWithRetry executes a node, retrying on failure according to the
// supplied RetryPolicy.  Panics from the executor are caught and returned
// as errors so that DB persistence in the caller is not bypassed.
//
// Errors a retry cannot fix (isNonRetryable) are returned after one attempt.
// Other errors are retried per policy, consulting RetryClassifier (when
// installed) before each retry: it may stop retrying or override the delay.
func executeWithRetry(
	ctx context.Context,
	executor NodeExecutor,
	input NodeInput,
	config map[string]interface{},
	policy RetryPolicy,
	observers ...func([]NodeOutput),
) (outputs []NodeOutput, err error) {
	maxRetries := policy.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries > 10 {
		maxRetries = 10
	}

	var lastErr error
	var override time.Duration // classifier-chosen delay for the next retry
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := computeDelay(attempt, policy)
			if override > 0 {
				delay = override
			}
			if retrySleep(ctx, delay) != nil || ctx.Err() != nil {
				return nil, ErrExecutionCancelled
			}
		}

		outputs, err = func() (out []NodeOutput, execErr error) {
			defer func() {
				if r := recover(); r != nil {
					execErr = fmt.Errorf("node executor panicked: %v\n%s", r, debug.Stack())
				}
			}()
			if out, handled, gerr := RunExecGuard(ctx, executor.Type(), input, config); handled {
				return out, gerr
			}
			return executor.Execute(ctx, input, config)
		}()
		for _, observer := range observers {
			observer(outputs)
		}
		if err == nil {
			return outputs, nil
		}
		lastErr = err

		// Do not retry if the context is done.
		select {
		case <-ctx.Done():
			return nil, ErrExecutionCancelled
		default:
		}

		// A pause, invalid config, cancellation or Permanent error cannot be
		// fixed by running the node again: return it as-is (the caller relies
		// on errors.Is(err, ErrNodePaused) to suspend the run).
		if isNonRetryable(err) {
			return nil, err
		}
		if attempt == maxRetries {
			break // no retry left: never consult the classifier for nothing
		}
		retry, delay := classifyRetry(ctx, executor.Type(), err, attempt+1, config)
		if !retry {
			return nil, err
		}
		override = delay
	}
	return nil, lastErr
}

// computeDelay returns the sleep duration before the given retry attempt
// (attempt is 1-based: first retry is attempt=1).
func computeDelay(attempt int, p RetryPolicy) time.Duration {
	const maxDelaySeconds = 3600.0

	var seconds float64
	switch strings.ToLower(p.BackoffType) {
	case "exponential":
		// p.InitialDelay * 2^(attempt-1)
		seconds = p.InitialDelay * math.Pow(2, float64(attempt-1))
	default: // "fixed" or anything else
		seconds = p.InitialDelay
	}

	if seconds > maxDelaySeconds {
		seconds = maxDelaySeconds
	}
	if seconds < 0 {
		seconds = 0
	}

	return time.Duration(seconds * float64(time.Second))
}

// extractRetryPolicy reads RetryPolicy fields from the resolved config map.
// Defaults: MaxRetries=0, BackoffType="fixed", InitialDelay=1.
func extractRetryPolicy(config map[string]interface{}) RetryPolicy {
	p := RetryPolicy{
		MaxRetries:   0,
		BackoffType:  "fixed",
		InitialDelay: 1,
	}

	rpRaw, ok := config["retry_policy"]
	if !ok {
		return p
	}

	// Try to round-trip through JSON to get a clean RetryPolicy struct.
	b, err := json.Marshal(rpRaw)
	if err != nil {
		return p
	}
	var parsed RetryPolicy
	if err := json.Unmarshal(b, &parsed); err != nil {
		return p
	}
	if parsed.MaxRetries > 0 {
		p.MaxRetries = parsed.MaxRetries
	}
	if parsed.BackoffType != "" {
		p.BackoffType = parsed.BackoffType
	}
	if parsed.InitialDelay > 0 {
		p.InitialDelay = parsed.InitialDelay
	}
	return p
}
