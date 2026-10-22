package applypark

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/applications"
)

// ReopenNote is what the application page shows when its tab is gone.
const ReopenNote = "Reopen and refill: the browser tab with the filled form was closed."

// SweepResult says what a sweep changed.
type SweepResult struct {
	Closed []string `json:"closed"` // applications that are over: their tab binding is closed
	Lost   []string `json:"lost"`   // applications whose tab is gone: now needs_you
}

// Sweep reconciles the live bindings against the browser's tabs. An
// application that is no longer being worked on (applied, rejected,
// cancelled) has its binding closed. A binding whose tab is not in live
// becomes lost and its application needs_you with ReopenNote (a new task
// refills it). live must be a successful read of `browser tabs`: with the
// browser unreachable do not sweep, nothing is known. Sweep never sets
// applied.
func Sweep(ctx context.Context, store Store, apps *applications.Store, live []LiveTab) (SweepResult, error) {
	res := SweepResult{Closed: []string{}, Lost: []string{}}
	open := map[int]bool{}
	for _, t := range live {
		open[t.TabID] = true
	}
	tabs, err := store.Live(ctx)
	if err != nil {
		return res, err
	}
	for _, t := range tabs {
		app, err := apps.Get(ctx, store.Profile, t.ApplicationID)
		if err != nil {
			return res, err
		}
		switch app.Status {
		case applications.StatusAwaitingSubmit, applications.StatusNeedsYou, applications.StatusPending:
		default:
			if err := store.SetState(ctx, t.ApplicationID, StateClosed, ""); err != nil {
				return res, err
			}
			res.Closed = append(res.Closed, t.ApplicationID)
			continue
		}
		if open[t.TabID] {
			continue
		}
		if err := store.SetState(ctx, t.ApplicationID, StateLost, ""); err != nil {
			return res, err
		}
		if err := markReopen(ctx, apps, store.Profile, app); err != nil {
			return res, fmt.Errorf("application %s: %w", t.ApplicationID, err)
		}
		res.Lost = append(res.Lost, t.ApplicationID)
	}
	return res, nil
}

// markReopen turns the application into needs_you with ReopenNote, keeping its
// field report.
func markReopen(ctx context.Context, apps *applications.Store, profile string, app *applications.Application) error {
	fill, err := apps.GetFill(ctx, profile, app.ID)
	if err != nil {
		return err
	}
	fill.Status, fill.Note = applications.FillNeedsYou, ReopenNote
	if err := apps.RecordFill(ctx, profile, app.ID, fill); err != nil {
		return err
	}
	if app.Status == applications.StatusNeedsYou {
		return nil
	}
	return apps.SetStatus(ctx, profile, app.ID, applications.StatusNeedsYou, applications.ActorSystem, ReopenNote)
}
