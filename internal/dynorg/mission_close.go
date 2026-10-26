package dynorg

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/tasks"
)

// closeRefusal is why a mission is not ready to close.
type closeRefusal struct{ why string }

func (e *closeRefusal) Error() string { return "the session is not closed: " + e.why }

// finalReportRef is the reference of the final report artifact; its presence on the
// integration task is the record that the close was already proposed.
const finalReportRef = "final-report"

// readyToClose is the predicate for proposing a mission's close; the conductor never closes
// a session, the operator does. It reads the session and its plan afresh from the store and
// refuses unless the session is running, every non-epic task is done or verified, no task is
// blocked, no writer branch is waiting for its merge, and the integration check task exists,
// is done or verified, and is newer than every other task. It returns the integration task.
func (c *Conductor) readyToClose(ctx context.Context, mc *MissionRun, sid int64) (tasks.Task, error) {
	se, err := mc.Store.GetSession(ctx, mc.ProfileID, sid, missionActor())
	if err != nil {
		return tasks.Task{}, err
	}
	if se.State != tasks.SessionRunning || se.Archived {
		return tasks.Task{}, &closeRefusal{fmt.Sprintf("session #%d is %s, not running", sid, se.State)}
	}
	plan, err := mc.Store.PlanOfSession(ctx, mc.ProfileID, sid, missionActor())
	if err != nil {
		return tasks.Task{}, err
	}
	var integration *tasks.Task
	for i, t := range plan.Tasks {
		if t.Blocked {
			return tasks.Task{}, &closeRefusal{fmt.Sprintf("task #%d is blocked", t.ID)}
		}
		if integration == nil && isIntegration(t) {
			integration = &plan.Tasks[i]
		}
	}
	for _, t := range plan.Tasks {
		if t.Kind != tasks.KindEpic && !settled(t) {
			return tasks.Task{}, &closeRefusal{fmt.Sprintf("task #%d is %s, not done or verified", t.ID, t.Status)}
		}
	}
	if integration == nil {
		return tasks.Task{}, &closeRefusal{"the plan has no integration check task"}
	}
	for _, t := range plan.Tasks {
		if t.Kind == tasks.KindEpic {
			continue
		}
		if who, bad := c.verdictUntrusted(ctx, mc, sid, t); bad {
			return tasks.Task{}, &closeRefusal{fmt.Sprintf("task #%d was judged by %q, not by its verifier (%s)", t.ID, who, verdictByUnexpectedActor)}
		}
	}
	if m := c.mergesRemaining(ctx, mc, sid, plan.Tasks); len(m) > 0 {
		return tasks.Task{}, &closeRefusal{fmt.Sprintf("%d writer branch(es) are not merged yet", len(m))}
	}
	if why := c.integrationStale(ctx, mc, sid, plan, *integration); why != "" {
		return tasks.Task{}, &closeRefusal{why}
	}
	return *integration, nil
}

// proposeClose attaches the final report to the integration task as the record that the
// mission is ready for the operator to close, and leaves the session running. It refuses
// unless readyToClose holds. proposed is false when the report was already attached by an
// earlier call: nothing is written then.
func (c *Conductor) proposeClose(ctx context.Context, mc *MissionRun, sid int64, report string) (proposed bool, err error) {
	integration, err := c.readyToClose(ctx, mc, sid)
	if err != nil {
		return false, err
	}
	if d, err := mc.Store.SessionTask(ctx, mc.ProfileID, sid, integration.ID, missionActor()); err == nil {
		for _, a := range d.Artifacts {
			if a.Kind == "report" && a.Ref == finalReportRef {
				return false, nil
			}
		}
	}
	art := tasks.ArtifactInput{Kind: "report", Ref: finalReportRef, Note: report}
	if _, err := mc.Store.AddArtifact(ctx, mc.ProfileID, sid, integration.ID, art, operatorActor()); err != nil {
		return false, fmt.Errorf("the final report could not be attached to task #%d: %w", integration.ID, err)
	}
	return true, nil
}
