// Package workflowfix turns the result of a failed-run-fix task into the exact
// edits to a workflow, for the user to review (Plan) and then apply (Apply).
// The agent that wrote the result only proposed; nothing here runs unless the
// operator asks, and an applied edit leaves the workflow UNAPPROVED: the
// definition changes, so the stored approval (keyed by the definition's hash,
// internal/workflow/approvals.go) no longer matches, and nothing here records a
// new one. The user approves by saving or activating the workflow themselves.
package workflowfix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Fix kinds in a failed-run-fix result.
const (
	KindWorkflowEdit = "workflow-edit"
	KindAdvice       = "advice"
	KindNone         = "none"
)

// MaxEdits is how many node edits a result may carry.
const MaxEdits = 10

// Warning codes.
const (
	WarnWorkflowActive = "workflow_active"
	WarnNewOutbound    = "new_outbound"
	WarnCredentialKey  = "credential_field"
	WarnExpressionAdd  = "expression_added"
)

// Error codes (Error.Code).
const (
	CodeBadResult      = "bad_result"
	CodeNoEdit         = "no_edit"
	CodeNoChange       = "no_change"
	CodeNotFound       = "workflow_not_found"
	CodeUnknownNode    = "unknown_node"
	CodeSecretKey      = "secret_key"
	CodeInvalid        = "invalid_workflow"
	CodeWorkflowActive = "workflow_active_needs_confirm"
	CodeStale          = "stale_proposal"
	CodeNoBackend      = "no_backend"
)

// Error is a refusal with a code the CLI and the GUI show.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

func fail(code, format string, a ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Backend reads and writes the profile's workflows. The CLI sets it; with none
// every Plan and Apply is refused (never trusted).
type Backend interface {
	// Load returns workflow id when profile owns it, else (nil, nil).
	Load(ctx context.Context, profile, id string) (*workflow.Workflow, error)
	// SaveNodes stores the nodes of an existing workflow, as `workflow node set`
	// does, and records no approval.
	SaveNodes(ctx context.Context, profile, id string, nodes []workflow.WorkflowNode) error
}

// Store is the Backend in force, and NodeTypeKnown the node-type hook of the
// validator (what `workflow validate` uses); both are set by the CLI at start.
var (
	Store         Backend
	NodeTypeKnown func(string) bool
)

// Edit is one change of one config path of one node.
type Edit struct {
	Node     string `json:"node"`
	NodeName string `json:"nodeName"`
	Path     string `json:"path"`
	Before   any    `json:"before"`
	After    any    `json:"after"`
}

// Warning is something the user should look at before applying.
type Warning struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Preview is what applying a result would do to the workflow as it is now.
type Preview struct {
	WorkflowID   string    `json:"workflowId"`
	WorkflowName string    `json:"workflowName"`
	Active       bool      `json:"active"`
	Edits        []Edit    `json:"edits"`
	Warnings     []Warning `json:"warnings"`

	nodes []workflow.WorkflowNode // the whole node list after the edits
}

type proposal struct {
	WorkflowID string `json:"workflowId"`
	Fix        struct {
		Kind  string `json:"kind"`
		Edits []struct {
			Node   string         `json:"node"`
			Config map[string]any `json:"config"`
		} `json:"edits"`
	} `json:"fix"`
}

var secretKey = regexp.MustCompile(`(?i)(pass(word|wd|phrase)?|secret|token|api[_-]?key|apikey|authorization|bearer|private[_-]?key|credential|cookie|session[_-]?id)`)

// credentialRef keys name a saved connection, not a secret.
var credentialRef = map[string]bool{"credential_id": true, "credentialid": true, "connection_id": true}

// Plan computes the edits of result (a failed-run-fix result) against the
// workflow as it is now, in profile. It changes nothing.
func Plan(ctx context.Context, profile string, result []byte) (Preview, error) {
	return PlanSince(ctx, profile, result, time.Time{})
}

// modified is when wf, or any of its nodes, last changed.
func modified(wf *workflow.Workflow) time.Time {
	t := wf.UpdatedAt
	for _, n := range wf.Nodes {
		if n.UpdatedAt.After(t) {
			t = n.UpdatedAt
		}
	}
	return t
}

// PlanSince is Plan for a proposal that was made at since (when the task
// completed; zero skips the check): a workflow changed after that is stale_proposal,
// because the person who looked at the proposal did not look at this workflow.
func PlanSince(ctx context.Context, profile string, result []byte, since time.Time) (Preview, error) {
	var p proposal
	if err := json.Unmarshal(result, &p); err != nil {
		return Preview{}, fail(CodeBadResult, "the result is not a failed-run-fix result: %v", err)
	}
	if p.Fix.Kind != KindWorkflowEdit {
		return Preview{}, fail(CodeNoEdit, "this fix is %q, not a workflow edit: there is nothing to apply", p.Fix.Kind)
	}
	if len(p.Fix.Edits) == 0 || len(p.Fix.Edits) > MaxEdits {
		return Preview{}, fail(CodeBadResult, "a workflow-edit fix needs 1 to %d edits", MaxEdits)
	}
	if Store == nil {
		return Preview{}, fail(CodeNoBackend, "workflows cannot be read here")
	}
	wf, err := Store.Load(ctx, profile, p.WorkflowID)
	if err != nil {
		return Preview{}, err
	}
	if wf == nil {
		return Preview{}, fail(CodeNotFound, "no workflow %q in this profile", p.WorkflowID)
	}
	if !since.IsZero() && modified(wf).After(since) {
		return Preview{}, fail(CodeStale, "the workflow changed since this fix was proposed: ask again")
	}
	baseline := validate(wf)

	nodes := append([]workflow.WorkflowNode(nil), wf.Nodes...)
	plan := Preview{WorkflowID: wf.ID, WorkflowName: wf.Name, Active: wf.IsActive, Edits: []Edit{}, Warnings: []Warning{}}
	var changed []workflow.WorkflowNode
	for _, e := range p.Fix.Edits {
		i := indexOf(nodes, e.Node)
		if i < 0 {
			return Preview{}, fail(CodeUnknownNode, "the workflow has no node %q", e.Node)
		}
		before := nodes[i].Config
		after := deepMerge(before, e.Config)
		diffs := diff("", before, after)
		for _, d := range diffs {
			if err := checkNew(d); err != nil {
				return Preview{}, err
			}
			plan.Edits = append(plan.Edits, Edit{Node: nodes[i].ID, NodeName: nodes[i].Name, Path: d.path, Before: d.before, After: d.after})
			plan.Warnings = append(plan.Warnings, warningsFor(nodes[i], d)...)
		}
		if len(diffs) == 0 {
			continue
		}
		nodes[i].Config = after
		if err := nodes[i].MarshalConfig(); err != nil {
			return Preview{}, fail(CodeBadResult, "node %q: %v", e.Node, err)
		}
		changed = append(changed, nodes[i])
	}
	if len(plan.Edits) == 0 {
		return Preview{}, fail(CodeNoChange, "the edits change nothing in the workflow as it is now (already applied?)")
	}
	if outbound := orggrant.OutboundNodes(&workflow.Workflow{Nodes: changed}); len(outbound) > 0 {
		plan.Warnings = append(plan.Warnings, Warning{WarnNewOutbound, "the edit changes a step that acts outside this computer: " + strings.Join(outbound, ", ")})
	}
	if wf.IsActive {
		plan.Warnings = append(plan.Warnings, Warning{WarnWorkflowActive, "this workflow is active: applying the edit changes what its triggers run"})
	}
	after := *wf
	after.Nodes = nodes
	if fresh := newErrors(baseline, validate(&after)); len(fresh) > 0 {
		return Preview{}, fail(CodeInvalid, "the edited workflow does not pass `workflow validate`:\n- %s", strings.Join(fresh, "\n- "))
	}
	plan.nodes = nodes
	plan.Warnings = dedupe(plan.Warnings)
	return plan, nil
}

// Options say how Apply runs.
type Options struct {
	// AllowActive lets the edit touch an active workflow (the operator confirmed).
	AllowActive bool
	// Since is when the proposal was made; a workflow changed after it is stale.
	Since time.Time
}

// Apply plans result again against the workflow as it is now and stores the
// edits. An active workflow is refused unless opts.AllowActive. The workflow stays
// unapproved: no approval is recorded. It returns the number of edits applied.
func Apply(ctx context.Context, profile string, result []byte, opts Options) (int, error) {
	plan, err := PlanSince(ctx, profile, result, opts.Since)
	if err != nil {
		return 0, err
	}
	if plan.Active && !opts.AllowActive {
		return 0, fail(CodeWorkflowActive, "workflow %q is active: review the edit, then confirm to apply it", plan.WorkflowName)
	}
	if err := Store.SaveNodes(ctx, profile, plan.WorkflowID, plan.nodes); err != nil {
		return 0, fmt.Errorf("save nodes: %w", err)
	}
	return len(plan.Edits), nil
}

func validate(wf *workflow.Workflow) []string {
	cp := *wf
	cp.Nodes = append([]workflow.WorkflowNode(nil), wf.Nodes...)
	cp.Connections = append([]workflow.WorkflowConnection(nil), wf.Connections...)
	errs, _ := generate.Validator(NodeTypeKnown)(&cp)
	return errs
}

func newErrors(baseline, now []string) []string {
	seen := map[string]bool{}
	for _, b := range baseline {
		seen[b] = true
	}
	var out []string
	for _, n := range now {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

func indexOf(nodes []workflow.WorkflowNode, id string) int {
	for i := range nodes {
		if nodes[i].ID == id {
			return i
		}
	}
	return -1
}

type change struct {
	path          string
	before, after any
}

// deepMerge returns base with patch merged in: objects merge key by key,
// anything else is replaced. Neither argument is modified.
func deepMerge(base, patch map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(patch))
	for k, v := range base {
		out[k] = v
	}
	for k, pv := range patch {
		if pm, ok := pv.(map[string]any); ok {
			if bm, ok := out[k].(map[string]any); ok {
				out[k] = deepMerge(bm, pm)
				continue
			}
		}
		out[k] = pv
	}
	return out
}

// diff lists the leaf paths whose value differs between a and b.
func diff(prefix string, a, b map[string]any) []change {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []change
	for _, k := range sorted {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		av, aok := a[k]
		bv, bok := b[k]
		am, aIsMap := av.(map[string]any)
		bm, bIsMap := bv.(map[string]any)
		switch {
		case bIsMap && (aIsMap || !aok):
			out = append(out, diff(path, am, bm)...)
		case aok && bok && reflect.DeepEqual(av, bv):
		default:
			out = append(out, change{path: path, before: av, after: bv})
		}
	}
	return out
}

// checkNew refuses an edit that adds a key which looks like a secret, or a
// value that redacts to something else (it holds a secret).
func checkNew(c change) error {
	if c.before == nil {
		for _, part := range strings.Split(c.path, ".") {
			if secretKey.MatchString(part) && !credentialRef[strings.ToLower(part)] {
				return fail(CodeSecretKey, "the edit adds %q, which looks like a secret: a fix never carries one (use a saved connection)", c.path)
			}
		}
	}
	if s, ok := c.after.(string); ok && aitask.RedactText(s) != s {
		return fail(CodeSecretKey, "the edit puts a secret-looking value at %q", c.path)
	}
	return nil
}

func warningsFor(n workflow.WorkflowNode, c change) []Warning {
	var out []Warning
	for _, part := range strings.Split(c.path, ".") {
		if secretKey.MatchString(part) {
			out = append(out, Warning{WarnCredentialKey, fmt.Sprintf("node %q: the edit touches %q, a credential field", n.ID, c.path)})
			break
		}
	}
	if s, ok := c.after.(string); ok && strings.Contains(s, "{{") {
		if b, _ := c.before.(string); !strings.Contains(b, "{{") {
			out = append(out, Warning{WarnExpressionAdd, fmt.Sprintf("node %q: the edit adds an expression at %q", n.ID, c.path)})
		}
	}
	return out
}

func dedupe(ws []Warning) []Warning {
	seen := map[Warning]bool{}
	out := ws[:0:0]
	for _, w := range ws {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// IsCode reports whether err is a refusal with this code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Exists reports whether profile owns workflow id.
func Exists(ctx context.Context, profile, id string) (bool, error) {
	if Store == nil {
		return false, fail(CodeNoBackend, "workflows cannot be read here")
	}
	wf, err := Store.Load(ctx, profile, id)
	return wf != nil, err
}
