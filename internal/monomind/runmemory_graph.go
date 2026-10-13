package monomind

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RunOutcome aggregates a workflow's recent runs.
type RunOutcome struct {
	Total, Success, Failed, Partial, Cancelled int
	// Classes counts failure classes over failed and partial runs.
	Classes map[string]int
	// LatestOK is whether the newest non-cancelled run succeeded.
	LatestOK bool
	// LastFailure is the newest failed or partial run, its Error redacted
	// and shortened and its ErrorClass set.
	LastFailure *RunRecord
}

// AggregateRuns summarises runs (newest first). redact is applied to errors.
func AggregateRuns(runs []RunRecord, redact func(string) string) RunOutcome {
	o := RunOutcome{Classes: map[string]int{}, LatestOK: true}
	latestSet := false
	for _, r := range runs {
		o.Total++
		failed := false
		switch r.Status {
		case "SUCCESS":
			o.Success++
		case "SUCCESS_WITH_ERRORS":
			o.Partial++
			failed = true
		case "CANCELLED":
			o.Cancelled++
			continue
		default:
			o.Failed++
			failed = true
		}
		if !latestSet {
			o.LatestOK, latestSet = !failed, true
		}
		if !failed {
			continue
		}
		class := r.ErrorClass
		if !KnownFailureClass(class) {
			class = ClassifyRunError(r.Error)
		}
		o.Classes[class]++
		if o.LastFailure == nil {
			lf := r
			lf.ErrorClass = class
			lf.Error = shortError(redact(r.Error), maxRunErrorChars)
			lf.FailedNode = shortError(redact(r.FailedNode), 80)
			o.LastFailure = &lf
		}
	}
	return o
}

// Signature changes only when the outcome does: the newest run's
// success/failure and the set of failure classes.
func (o RunOutcome) Signature() string {
	state := "ok"
	if !o.LatestOK {
		state = "failing"
	}
	return state + "|" + strings.Join(o.classNames(), ",")
}

// classNames are the classes, most frequent first.
func (o RunOutcome) classNames() []string {
	names := make([]string, 0, len(o.Classes))
	for c := range o.Classes {
		names = append(names, c)
	}
	sort.Slice(names, func(i, j int) bool {
		if o.Classes[names[i]] != o.Classes[names[j]] {
			return o.Classes[names[i]] > o.Classes[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// Describe renders the outcome, e.g. "wf X: last 20 runs 17 success, 3
// failed: browser_unavailable ×2, upstream_5xx ×1; last failure <time> at
// node <name>: <short error>".
func (o RunOutcome) Describe(workflowName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "wf %s: last %d runs %d success", workflowName, o.Total, o.Success)
	if o.Failed > 0 {
		fmt.Fprintf(&b, ", %d failed", o.Failed)
	}
	if o.Partial > 0 {
		fmt.Fprintf(&b, ", %d with errors", o.Partial)
	}
	if o.Cancelled > 0 {
		fmt.Fprintf(&b, ", %d cancelled", o.Cancelled)
	}
	if len(o.Classes) > 0 {
		parts := make([]string, 0, len(o.Classes))
		for _, c := range o.classNames() {
			parts = append(parts, fmt.Sprintf("%s ×%d", c, o.Classes[c]))
		}
		b.WriteString(": " + strings.Join(parts, ", "))
	}
	if lf := o.LastFailure; lf != nil {
		b.WriteString("; last failure")
		if !lf.FinishedAt.IsZero() {
			b.WriteString(" " + lf.FinishedAt.UTC().Format(time.RFC3339))
		}
		if lf.FailedNode != "" {
			b.WriteString(" at node " + lf.FailedNode)
		}
		if lf.Error != "" {
			b.WriteString(": " + lf.Error)
		}
	}
	if o.LatestOK {
		b.WriteString("; latest run succeeded")
	} else {
		b.WriteString("; latest run failed")
	}
	return b.String()
}

// RunFix is a detected fix: the workflow was edited after failing with Class
// and its next run succeeded.
type RunFix struct {
	Class    string
	FailedAt time.Time
	EditedAt time.Time
}

// KG entity names.
func WorkflowEntity(name string) string      { return "workflow:" + name }
func RunOutcomeEntity(name string) string    { return "run outcome:" + name }
func FailureClassEntity(class string) string { return "failure class:" + class }
func NodeTypeEntity(t string) string         { return "node type:" + t }

// BuildRunMemoryGraph returns the entities and relations for one workflow.
func BuildRunMemoryGraph(wf *RunWorkflow, o RunOutcome, fix *RunFix) ([]KGNode, []KGEdge) {
	wfName := WorkflowEntity(wf.Name)
	types := uniqueSorted(wf.NodeTypes)
	desc := strings.TrimSpace(wf.Description)
	if len(types) > 0 {
		if desc != "" {
			desc += " "
		}
		desc += "Node types: " + strings.Join(types, ", ") + "."
	}
	nodes := []KGNode{
		{Name: wfName, Type: "workflow", Description: desc},
		{Name: RunOutcomeEntity(wf.Name), Type: "run_outcome", Description: o.Describe(wf.Name)},
	}
	edges := []KGEdge{{Source: wfName, Target: RunOutcomeEntity(wf.Name), Relation: "has_outcome"}}
	for _, t := range types {
		nodes = append(nodes, KGNode{Name: NodeTypeEntity(t), Type: "node_type", Description: "workflow node type " + t})
		edges = append(edges, KGEdge{Source: wfName, Target: NodeTypeEntity(t), Relation: "uses"})
	}
	for _, c := range o.classNames() {
		nodes = append(nodes, KGNode{Name: FailureClassEntity(c), Type: "failure_class", Description: failureClassDescriptions[c]})
		edges = append(edges, KGEdge{Source: wfName, Target: FailureClassEntity(c), Relation: "fails_with",
			Description: fmt.Sprintf("%d of the last %d runs", o.Classes[c], o.Total)})
	}
	if fix != nil {
		what := "workflow edited"
		if fix.Class == ClassSelectorMissing {
			what = "selector replaced (workflow edited)"
		}
		name := fmt.Sprintf("fix:%s %s", wf.Name, fix.EditedAt.UTC().Format(time.RFC3339))
		d := fmt.Sprintf("%s at %s after a failure", what, fix.EditedAt.UTC().Format(time.RFC3339))
		if fix.Class != "" {
			d += " (" + fix.Class + ")"
		}
		nodes = append(nodes, KGNode{Name: name, Type: "fix", Description: d + "; the next run succeeded"})
		edges = append(edges, KGEdge{Source: wfName, Target: name, Relation: "fix", Description: d})
		if fix.Class != "" {
			nodes = append(nodes, KGNode{Name: FailureClassEntity(fix.Class), Type: "failure_class", Description: failureClassDescriptions[fix.Class]})
			edges = append(edges, KGEdge{Source: name, Target: FailureClassEntity(fix.Class), Relation: "resolves"})
		}
	}
	return nodes, edges
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func dedupeNodes(in []KGNode) []KGNode {
	seen := map[string]bool{}
	var out []KGNode
	for _, n := range in {
		if !seen[n.Name] {
			seen[n.Name] = true
			out = append(out, n)
		}
	}
	return out
}

func dedupeEdges(in []KGEdge) []KGEdge {
	seen := map[string]bool{}
	var out []KGEdge
	for _, e := range in {
		k := e.Source + "\x00" + e.Relation + "\x00" + e.Target
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}
