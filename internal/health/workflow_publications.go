package health

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Workflow definitions live in a file and in database rows; a change is published to both through
// an intent row (see workflow.HybridWorkflowStore.Publish). This check reads those rows.
const (
	CheckWorkflowPublications = "workflows.publications"

	FixWorkflowPublicationsRecover = "workflows.publications.recover"
	// FixWorkflowPublicationsResolve is parameterized: "<workflow id>:keep-rows" or "<workflow id>:keep-file".
	FixWorkflowPublicationsResolve = "workflows.publications.resolve"
)

// WorkflowPublicationsInfo is what the check reads (all local).
type WorkflowPublicationsInfo struct {
	// Open lists the changes that are not finished.
	Open []OpenWorkflowPublication
	// Unreadable lists workflow files that do not parse; Pending lists valid files whose content
	// has not been reconciled with the database yet (an outside edit waiting for the next import).
	Unreadable []string
	Pending    []string
	// ImportedInactive lists workflows that appeared as outside files and were imported inactive.
	ImportedInactive []string
}

// OpenWorkflowPublication is one unfinished change.
type OpenWorkflowPublication struct {
	WorkflowID string
	Kind       string
	Origin     string
	State      string // pending | unresolved
	Age        time.Duration
	// OwnerGone: the process that started it is no longer running, or has held it past its lease.
	OwnerGone bool
	Note      string
}

func workflowPublicationChecks() []Check {
	return []Check{{ID: CheckWorkflowPublications, Group: GroupCore, Title: "Workflow files and database",
		DependsOn: []string{CheckDB}, Run: checkWorkflowPublications}}
}

func workflowPublicationFixes() []Fix {
	return []Fix{{FixInfo: FixInfo{ID: FixWorkflowPublicationsRecover,
		Label: "Put workflow files back in step with the database", Safety: SafetyAuto},
		Apply: fixWorkflowPublications},
		{FixInfo: FixInfo{ID: FixWorkflowPublicationsResolve, Label: "Resolve the unfinished change of {arg}",
			Safety: SafetyConfirm, Command: "monoagentcli doctor fix workflows.publications.resolve:{arg}"},
			ApplyArg: fixWorkflowPublicationResolve}}
}

func fixWorkflowPublicationResolve(ctx context.Context, env *Env, arg string, progress func(string)) error {
	i := strings.LastIndex(arg, ":")
	if i <= 0 || env.ResolveWorkflowPublication == nil {
		return fmt.Errorf("use workflows.publications.resolve:<workflow id>:keep-rows or :keep-file")
	}
	id, keep := arg[:i], arg[i+1:]
	progress("resolving " + id + " (" + keep + ")")
	return env.ResolveWorkflowPublication(ctx, id, keep)
}

func checkWorkflowPublications(ctx context.Context, env *Env) Result {
	if env.WorkflowPublications == nil {
		return Result{Status: StatusSkip, Summary: "not available"}
	}
	info, err := env.WorkflowPublications(ctx)
	if err != nil {
		return Result{Status: StatusWarn, Summary: "cannot read the workflow change records", Detail: err.Error()}
	}
	res := Result{Status: StatusOK, Summary: "workflow files and database agree"}
	stale, unresolved := 0, 0
	for _, o := range info.Open {
		child := Result{ID: CheckWorkflowPublications + "." + rowID(o.WorkflowID), Title: o.WorkflowID}
		switch {
		case o.State == "unresolved":
			unresolved++
			child.Status = StatusWarn
			child.Summary = "the file changed while a change was in progress; choose which to keep"
			child.Detail = o.Note + "\nkeep the committed rows (rewrites the file): monoagentcli doctor fix workflows.publications.resolve:" + o.WorkflowID + ":keep-rows" +
				"\nkeep the file as it is (the rows follow it): monoagentcli doctor fix workflows.publications.resolve:" + o.WorkflowID + ":keep-file"
			// No default fix: either choice discards something, so the person picks (see Detail).
		case o.OwnerGone:
			stale++
			child.Status = StatusWarn
			child.Summary = fmt.Sprintf("a %s by %s did not finish (%s ago)", o.Kind, orDefault(o.Origin, "an earlier run"), o.Age.Round(time.Second))
			child.FixID = FixWorkflowPublicationsRecover
		default:
			child.Status = StatusInfo
			child.Summary = fmt.Sprintf("a %s is in progress", o.Kind)
		}
		res.Children = append(res.Children, child)
	}
	for _, id := range info.Unreadable {
		res.Children = append(res.Children, Result{ID: CheckWorkflowPublications + ".unreadable." + rowID(id), Title: id,
			Status: StatusWarn, Summary: "the workflow file does not parse; it is not used until it is fixed"})
	}
	for _, id := range info.Pending {
		res.Children = append(res.Children, Result{ID: CheckWorkflowPublications + ".pending." + rowID(id), Title: id,
			Status: StatusInfo, Summary: "edited outside the app; it is picked up by the next command that opens the workflows"})
	}
	for _, id := range info.ImportedInactive {
		res.Children = append(res.Children, Result{ID: CheckWorkflowPublications + ".imported." + rowID(id), Title: id,
			Status: StatusInfo, Summary: "imported from an outside file, inactive; activate it to use it"})
	}
	switch {
	case unresolved > 0 || stale > 0 || len(info.Unreadable) > 0:
		res.Status = StatusWarn
		var parts []string
		if stale > 0 {
			parts = append(parts, fmt.Sprintf("%d unfinished change(s)", stale))
			res.FixID = FixWorkflowPublicationsRecover
		}
		if unresolved > 0 {
			parts = append(parts, fmt.Sprintf("%d needing a choice", unresolved))
		}
		if n := len(info.Unreadable); n > 0 {
			parts = append(parts, fmt.Sprintf("%d unreadable file(s)", n))
		}
		res.Summary = strings.Join(parts, ", ")
	case len(res.Children) > 0:
		res.Status, res.Summary = StatusInfo, fmt.Sprintf("%d change(s) in progress or waiting", len(res.Children))
	}
	return res
}

func fixWorkflowPublications(ctx context.Context, env *Env, progress func(string)) error {
	if env.RecoverWorkflowPublications == nil {
		return fmt.Errorf("not available here")
	}
	progress("putting workflow files back in step with the database")
	n, err := env.RecoverWorkflowPublications(ctx)
	if err != nil {
		return err
	}
	progress(fmt.Sprintf("looked at %d unfinished change(s)", n))
	return nil
}
