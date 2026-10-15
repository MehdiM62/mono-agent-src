// Package capturehooks is what a process that owns the extension bridge
// wires into it for every capture that lands: the AI summary, the page-kind
// classification and the capture indexing queue, plus the bridge's startup
// catch-up sweep and the Ask panel's index status.
package capturehooks

import (
	"context"
	"database/sql"
	"time"

	"github.com/monoes/mono-agent/internal/capture"
	"github.com/monoes/mono-agent/internal/captureindex"
	"github.com/monoes/mono-agent/internal/capturesummary"
	"github.com/monoes/mono-agent/internal/extension"
)

// Deps are the CLI's pieces the hooks use.
type Deps struct {
	// Runtime is the agent runtime that writes summaries (`extension serve
	// --summary-runtime`, else $MONOAGENT_SUMMARY_RUNTIME, else the default);
	// read once, when the hooks are installed.
	Runtime func() string
	// NewClassifier builds the page-kind classifier's after-write handler.
	NewClassifier func(logf func(string, ...any)) func(*capture.Result)
	// OpenDB opens the database for one indexing pass or status read.
	OpenDB func() (*sql.DB, func(), error)
}

// Hooks is what Install wires into a bridge: the summarizer and the capture
// indexing queue. Close stops both.
type Hooks struct {
	summaries *capturesummary.Summarizer
	index     *captureindex.Queue
	open      func() (*sql.DB, func(), error)
}

// Close cancels a summary or an indexing pass in flight. A pass cut short
// leaves its capture unindexed, and the next bridge's startup sweep takes
// it up again.
func (h *Hooks) Close() {
	h.summaries.Close()
	h.index.Stop()
}

// SweepIndexAfter starts the bridge's catch-up pass over every profile's
// unindexed captures (sweepCaptureIndexAfter).
func (h *Hooks) SweepIndexAfter(ctx context.Context, delay time.Duration) {
	sweepCaptureIndexAfter(ctx, h.index, h.open, delay)
}

// Install wires a summarizer, the page-kind classifier and
// the capture indexing queue into srv's after-write hook and returns them.
// logf receives one line per summary and indexing outcome.
//
// The same catalog of installed runtimes backs the extension's "AI for
// summaries" picker (summary.runtimes / summary.models) and the check a
// capture's own runtime choice has to pass, so the picker never offers
// something the summarizer would then refuse.
func Install(srv *extension.Server, logf func(string, ...any), d Deps) *Hooks {
	runtime := d.Runtime()
	catalog := capturesummary.MonomindCatalog()
	sum := capturesummary.New(runtime, capturesummary.ExecRunner(0))
	sum.Catalog = catalog
	sum.Logf = logf
	// Page-kind classification (Jev surface "capture") runs after the
	// summary is queued; it is a no-op unless the capture's profile enabled
	// it, and never makes the capture wait (capture_classify.go).
	classify := d.NewClassifier(logf)
	// Indexing makes the capture searchable from Ask and chat within
	// seconds (index.go). Queued like the summary, so
	// the capture is acknowledged as fast as before.
	index := newCaptureIndexQueue(d.OpenDB, logf)
	// A summary lands minutes after its capture; indexing it is one more
	// pass over the capture's profile.
	sum.OnDone = func(dir string, _ capturesummary.Status) { handleIndexSummary(index, dir) }
	srv.SetAfterWrite(func(res *capture.Result) {
		sum.Handle(res)
		classify(res)
		handleIndexCapture(index, res)
	})
	srv.SetSummaryCatalog(catalog, runtime)
	srv.SetBrainStatusSource(func(ctx context.Context, profileID string) (*extension.BrainStatus, error) {
		return captureBrainStatus(ctx, d.OpenDB, profileID)
	})
	return &Hooks{summaries: sum, index: index, open: d.OpenDB}
}
