package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
)

// EnvRunID carries a run's id to child processes (daemon → CLI → monomind),
// so their log lines can be joined with the parent's.
const EnvRunID = "MONOAGENT_RUN_ID"

// KeyRunID is the log attribute holding the run id.
const KeyRunID = "run_id"

type ctxKey int

const (
	loggerKey ctxKey = iota
	runIDKey
)

// With returns ctx carrying FromContext(ctx).With(args...): every line
// logged through FromContext of the result has those attributes. When args
// set "run_id", RunID(ctx) reports it too.
func With(ctx context.Context, args ...any) context.Context {
	ctx = context.WithValue(ctx, loggerKey, FromContext(ctx).With(args...))
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == KeyRunID {
			if id, ok := args[i+1].(string); ok {
				ctx = context.WithValue(ctx, runIDKey, id)
			}
		}
	}
	return ctx
}

// WithRunID is With(ctx, "run_id", id); an empty id returns ctx unchanged.
// Call it where a workflow execution or org run starts.
func WithRunID(ctx context.Context, id string) context.Context {
	if id == "" || RunID(ctx) == id {
		return ctx
	}
	return With(ctx, KeyRunID, id)
}

// EnsureRunID is ctx with a run id: the one it (or this process's
// MONOAGENT_RUN_ID) has, else a new prefix+uuid. For runs whose own id is
// made elsewhere (monomind numbers org runs), so the lines of this process
// and of its children still share one.
func EnsureRunID(ctx context.Context, prefix string) context.Context {
	if RunID(ctx) != "" {
		return ctx
	}
	return WithRunID(ctx, prefix+uuid.NewString())
}

// RunID is the run id of ctx, else this process's MONOAGENT_RUN_ID ("" when
// neither is set).
func RunID(ctx context.Context) string {
	if ctx != nil {
		if id, ok := ctx.Value(runIDKey).(string); ok && id != "" {
			return id
		}
	}
	return os.Getenv(EnvRunID)
}

// FromContext is the logger With put in ctx, else the root logger.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// ChildEnv returns env (KEY=VALUE entries, as for exec.Cmd.Env) with
// MONOAGENT_RUN_ID set to RunID(ctx), replacing any value env had. With no
// run id it returns env unchanged.
func ChildEnv(ctx context.Context, env []string) []string {
	id := RunID(ctx)
	if id == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, EnvRunID+"=") {
			out = append(out, kv)
		}
	}
	return append(out, EnvRunID+"="+id)
}
