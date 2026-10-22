package extensioncli

import (
	"context"
	"errors"

	"github.com/monoes/mono-agent/internal/applypark"
	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/extension"
)

// submitSink is the host's half of form.submit_observed: it records the hint
// on the parked application (internal/applypark.Observe). It opens the
// database per request, as the task sink does, and never migrates it.
type submitSink struct{ path string }

// extensionSubmitSink is the sink newExtensionServer installs.
func extensionSubmitSink(path string) extension.SubmitSink { return submitSink{path: path} }

// Observed implements extension.SubmitSink.
func (s submitSink) Observed(ctx context.Context, o extension.SubmitObservation) (bool, error) {
	db, err := capturecli.OpenProfileDB(s.path)
	if err != nil {
		return false, extension.Unavailable("MonoAgent has no database yet: start MonoAgent or run any monoagentcli command")
	}
	defer db.Close()
	var tables int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'application_tabs'`).Scan(&tables); err != nil || tables == 0 {
		return false, nil // no parked application can exist yet
	}
	_, matched, err := applypark.Observe(ctx, db.DB, applypark.Observation{TabID: o.TabID, Origin: o.Origin, URL: o.URL})
	if errors.Is(err, applypark.ErrInvalid) {
		return false, &extension.RequestError{Code: extension.CodeInvalidInput, Err: err}
	}
	return matched, err
}
