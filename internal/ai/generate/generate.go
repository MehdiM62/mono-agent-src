package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// DefaultMaxAttempts is how many turns Generate takes when none is given.
const DefaultMaxAttempts = 3

// ExecFunc runs one model turn and returns its answer text.
type ExecFunc func(ctx context.Context, prompt, systemPrompt string) (string, error)

// ValidateFunc checks a parsed workflow. errs reject it (they go back to the
// model); warnings are reported but accepted.
type ValidateFunc func(wf *workflow.Workflow) (errs, warnings []string)

// Request is one generation.
type Request struct {
	Description string
	// Name, when set, replaces the name the model chose (before validation).
	Name        string
	MaxAttempts int
	Catalog     []NodeInfo
	Exec        ExecFunc
	Validate    ValidateFunc
}

// Attempt is one turn's outcome.
type Attempt struct {
	N      int      `json:"attempt"`
	Errors []string `json:"errors,omitempty"`
}

// Result is a validated workflow.
type Result struct {
	Workflow *workflow.Workflow
	// Document is the workflow in the documented file shape (`ref workflow`).
	Document workflow.WorkflowFile
	Attempts int
	History  []Attempt
	Warnings []string
}

// ErrNotValid is wrapped by the error Generate returns when no attempt
// produced a valid workflow.
var ErrNotValid = errors.New("no valid workflow")

// AttemptsError is Generate's error after the last failed attempt; it keeps
// each attempt's problems.
type AttemptsError struct {
	History []Attempt
}

func (e *AttemptsError) Error() string {
	last := []string{"(no detail)"}
	if n := len(e.History); n > 0 && len(e.History[n-1].Errors) > 0 {
		last = e.History[n-1].Errors
	}
	return fmt.Sprintf("%v after %d attempt(s); last problems: %s", ErrNotValid, len(e.History), strings.Join(last, "; "))
}

func (e *AttemptsError) Unwrap() error { return ErrNotValid }

// Generate asks the model for a workflow and retries with the problems until
// one validates or MaxAttempts turns have run. An Exec error (the runtime is
// missing, not logged in, the turn failed) ends it at once: retrying would
// fail the same way.
func Generate(ctx context.Context, req Request) (*Result, error) {
	if strings.TrimSpace(req.Description) == "" {
		return nil, fmt.Errorf("a description is required")
	}
	if req.Exec == nil || req.Validate == nil {
		return nil, fmt.Errorf("generate: Exec and Validate are required")
	}
	max := req.MaxAttempts
	if max <= 0 {
		max = DefaultMaxAttempts
	}
	system := SystemPrompt(req.Catalog)
	var (
		history  []Attempt
		previous string
		problems []string
	)
	for n := 1; n <= max; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		answer, err := req.Exec(ctx, userPrompt(req.Description, previous, problems), system)
		if err != nil {
			return nil, err
		}
		previous = strings.TrimSpace(answer)
		wf, perr := Parse(answer)
		if perr != nil {
			problems = []string{perr.Error()}
			history = append(history, Attempt{N: n, Errors: problems})
			continue
		}
		if req.Name != "" {
			wf.Name = req.Name
		}
		if wf.Description == "" {
			wf.Description = strings.TrimSpace(req.Description)
		}
		wf.IsActive = false
		errs, warnings := req.Validate(wf)
		if len(errs) > 0 {
			problems = errs
			history = append(history, Attempt{N: n, Errors: errs})
			continue
		}
		history = append(history, Attempt{N: n})
		return &Result{Workflow: wf, Document: ToFile(wf), Attempts: n, History: history, Warnings: warnings}, nil
	}
	return nil, &AttemptsError{History: history}
}

// Parse extracts the JSON document from a model answer and reads it as a
// workflow in the documented file shape.
func Parse(answer string) (*workflow.Workflow, error) {
	raw, err := ExtractJSON(answer)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.ParseWorkflowFileBytes([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("the answer is not a workflow JSON document: %v", err)
	}
	for _, n := range wf.Nodes {
		if n.Type == "" {
			return nil, fmt.Errorf("node %q has no \"type\"", n.ID)
		}
	}
	return &wf, nil
}

// ExtractJSON returns the first complete JSON object in text: the contents
// of a ```json fence when there is one, else the first balanced {...}.
func ExtractJSON(text string) (string, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return "", fmt.Errorf("the answer is empty")
	}
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
			if j := strings.Index(rest, "```"); j >= 0 {
				if obj, ok := firstObject(rest[:j]); ok {
					return obj, nil
				}
			}
		}
	}
	if obj, ok := firstObject(s); ok {
		return obj, nil
	}
	return "", fmt.Errorf("the answer contains no complete JSON object")
}

// firstObject scans for the first balanced, parseable JSON object.
func firstObject(s string) (string, bool) {
	for start := strings.IndexByte(s, '{'); start >= 0; {
		depth, inStr, esc := 0, false, false
		for i := start; i < len(s); i++ {
			c := s[i]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
			case c == '{':
				depth++
			case c == '}':
				depth--
				if depth == 0 {
					cand := s[start : i+1]
					if json.Valid([]byte(cand)) {
						return cand, true
					}
					i = len(s)
				}
			}
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return "", false
}

// ToFile is wf in the documented file shape (type/source/target keys).
func ToFile(wf *workflow.Workflow) workflow.WorkflowFile {
	f := workflow.WorkflowFile{
		ID:          wf.ID,
		Name:        wf.Name,
		Description: wf.Description,
		Version:     wf.Version,
		IsActive:    wf.IsActive,
		ProfileID:   wf.ProfileID,
		CreatedAt:   wf.CreatedAt,
		UpdatedAt:   wf.UpdatedAt,
		Nodes:       []workflow.WorkflowFileNode{},
		Connections: []workflow.WorkflowFileEdge{},
	}
	for _, n := range wf.Nodes {
		fn := workflow.WorkflowFileNode{ID: n.ID, Type: n.Type, Name: n.Name, Disabled: n.Disabled, Config: n.Config}
		fn.Position.X, fn.Position.Y = n.PositionX, n.PositionY
		f.Nodes = append(f.Nodes, fn)
	}
	for _, c := range wf.Connections {
		f.Connections = append(f.Connections, workflow.WorkflowFileEdge{
			ID: c.ID, Source: c.SourceNodeID, SourceHandle: c.SourceHandle,
			Target: c.TargetNodeID, TargetHandle: c.TargetHandle,
		})
	}
	return f
}

// NormalizeHandles defaults empty connection handles to "main", as `workflow
// import` and `workflow validate` do before validating.
func NormalizeHandles(wf *workflow.Workflow) {
	for i := range wf.Connections {
		if wf.Connections[i].SourceHandle == "" {
			wf.Connections[i].SourceHandle = "main"
		}
		if wf.Connections[i].TargetHandle == "" {
			wf.Connections[i].TargetHandle = "main"
		}
	}
}

// Validator is the validation `workflow validate` runs (handles normalized,
// then the schema-aware workflow.ValidateWorkflow: structure, required fields,
// select options, node references, expressions). With known set an unknown
// node type is an error: for a generated workflow it is an invented one, so
// the model must fix it. Each error carries its hint so the next attempt can
// act on it; warnings (e.g. needs_setup for an account) do not fail the draft.
// known reports whether a non-trigger type is registered; nil skips that check.
func Validator(known func(string) bool) ValidateFunc {
	return func(wf *workflow.Workflow) ([]string, []string) {
		NormalizeHandles(wf)
		rep := workflow.ValidateWorkflow(wf, workflow.ValidateOptions{KnownNodeType: known})
		var errs, warns []string
		for _, is := range rep.Issues {
			line := is.Message
			if is.NodeID != "" {
				line = fmt.Sprintf("node %q: %s", is.NodeID, line)
			}
			if is.Hint != "" {
				line += " (" + is.Hint + ")"
			}
			if is.Severity == "error" {
				errs = append(errs, line)
			} else {
				warns = append(warns, line)
			}
		}
		return errs, warns
	}
}
