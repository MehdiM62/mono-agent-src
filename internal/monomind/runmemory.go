package monomind

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Workflow-run memory: the profile knowledge graph learns how each workflow
// behaves — one outcome entity per workflow (an aggregate over its recent
// runs, not one node per run), the failure classes it hits, the node types it
// uses, and fixes (the workflow edited after a failure, then succeeding).
// Ingest is throttled per workflow and happens only when the outcome changes
// (a success/failure flip or a different set of failure classes), so the
// graph does not grow with every run.

// RunWorkflow is what run memory needs to know about a workflow.
type RunWorkflow struct {
	ID          string
	Name        string
	Description string
	ProfileID   string
	// NodeTypes are the workflow's node types (duplicates allowed).
	NodeTypes []string
	// UpdatedAt is when the definition last changed.
	UpdatedAt time.Time
}

// RunRecord is one finished run of a workflow.
type RunRecord struct {
	ExecutionID string
	// Status is the engine's terminal status: SUCCESS, FAILED,
	// SUCCESS_WITH_ERRORS or CANCELLED.
	Status     string
	FinishedAt time.Time
	// FailedNode names the first failed node, when one failed.
	FailedNode string
	// Error is the failed node's error, else the run's.
	Error string
	// ErrorClass is the engine's class for the failure; empty means derive it.
	ErrorClass string
}

// FinishedRun points at a run that finished at FinishedAt.
type FinishedRun struct {
	WorkflowID string
	ProfileID  string
	FinishedAt time.Time
}

// RunSource reads workflows and their runs. The CLI implements it over the
// workflow store; tests stub it.
type RunSource interface {
	// Workflow returns the workflow, or nil when it no longer exists.
	Workflow(ctx context.Context, id string) (*RunWorkflow, error)
	// RecentRuns returns up to n finished runs, newest first.
	RecentRuns(ctx context.Context, workflowID string, n int) ([]RunRecord, error)
	// FinishedSince returns runs finished at or after since.
	FinishedSince(ctx context.Context, since time.Time) ([]FinishedRun, error)
}

// SyncFunc has SyncToKnowledgeGraph's signature.
type SyncFunc func(ctx context.Context, db *sql.DB, profileID string, nodes []KGNode, edges []KGEdge, originRef string) error

const (
	// RunMemorySetting is the opt-out: `monoagentcli config set memory.runs off`.
	RunMemorySetting = "memory.runs"
	// runMemoryStateKey holds the sweeper's watermark and per-workflow state
	// in the settings table.
	runMemoryStateKey = "memory.runs.state"
	// DefaultRunMemoryInterval is the least time between two ingests of one workflow.
	DefaultRunMemoryInterval = 30 * time.Minute
	// DefaultRunMemoryWindow is how many recent runs an outcome aggregates.
	DefaultRunMemoryWindow = 20
	maxRunErrorChars       = 160
)

// RunMemory ingests workflow-run outcomes into the profile knowledge graph.
type RunMemory struct {
	DB     *sql.DB
	Source RunSource
	// Sync defaults to SyncToKnowledgeGraph.
	Sync SyncFunc
	// Redact defaults to RedactRunError.
	Redact func(string) string
	// Now defaults to time.Now.
	Now func() time.Time
	// MinInterval defaults to DefaultRunMemoryInterval; Window to DefaultRunMemoryWindow.
	MinInterval time.Duration
	Window      int

	mu sync.Mutex
}

// RunMemoryEnabled reports whether run memory is on: it is unless the
// memory.runs config is off/false/0/no/disabled.
func RunMemoryEnabled(db *sql.DB) bool {
	if db == nil {
		return false
	}
	var v string
	if err := db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, RunMemorySetting).Scan(&v); err != nil {
		return true
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(v), `"`)) {
	case "off", "false", "0", "no", "disabled":
		return false
	}
	return true
}

type runMemState struct {
	Watermark time.Time                 `json:"watermark"`
	Workflows map[string]*wfMemoryState `json:"workflows"`
}

type wfMemoryState struct {
	ProfileID     string    `json:"profile_id"`
	Signature     string    `json:"signature"`
	LastIngest    time.Time `json:"last_ingest"`
	Pending       bool      `json:"pending,omitempty"`
	LastOK        bool      `json:"last_ok"`
	LastFailAt    time.Time `json:"last_fail_at,omitempty"`
	LastFailClass string    `json:"last_fail_class,omitempty"`
}

// Observe re-evaluates one workflow after a run of it finished (the engine
// completion hook path). It is a no-op when run memory is off.
func (m *RunMemory) Observe(ctx context.Context, workflowID string) error {
	if workflowID == "" || !RunMemoryEnabled(m.DB) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.loadState()
	err := m.process(ctx, st, []string{workflowID})
	return errors.Join(err, m.saveState(st))
}

// Sweep ingests every workflow with a run finished since the stored
// watermark, plus workflows whose change was throttled earlier, then advances
// the watermark. It works without any engine hook.
func (m *RunMemory) Sweep(ctx context.Context) error {
	if !RunMemoryEnabled(m.DB) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.loadState()
	refs, err := m.Source.FinishedSince(ctx, st.Watermark)
	if err != nil {
		return fmt.Errorf("run memory: finished runs: %w", err)
	}
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	mark := st.Watermark
	for _, r := range refs {
		add(r.WorkflowID)
		if r.FinishedAt.After(mark) {
			mark = r.FinishedAt
		}
	}
	for id, ws := range st.Workflows {
		if ws.Pending {
			add(id)
		}
	}
	sort.Strings(ids)
	perr := m.process(ctx, st, ids)
	st.Watermark = mark
	return errors.Join(perr, m.saveState(st))
}

// process evaluates ids and ingests the changed ones with one sync per profile.
func (m *RunMemory) process(ctx context.Context, st *runMemState, ids []string) error {
	type batch struct {
		nodes   []KGNode
		edges   []KGEdge
		commits []func()
		ids     []string
	}
	batches := map[string]*batch{}
	var order []string
	var errs []error
	for _, id := range ids {
		upd, err := m.evaluate(ctx, st, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if upd == nil {
			continue
		}
		b := batches[upd.profileID]
		if b == nil {
			b = &batch{}
			batches[upd.profileID] = b
			order = append(order, upd.profileID)
		}
		b.nodes = append(b.nodes, upd.nodes...)
		b.edges = append(b.edges, upd.edges...)
		b.commits = append(b.commits, upd.commit)
		b.ids = append(b.ids, id)
	}
	for _, pid := range order {
		b := batches[pid]
		ref := "workflow-runs:" + strings.Join(b.ids, ",")
		if len(b.ids) > 1 {
			ref = fmt.Sprintf("workflow-runs:%s+%d", b.ids[0], len(b.ids)-1)
		}
		if err := m.sync()(ctx, m.DB, pid, dedupeNodes(b.nodes), dedupeEdges(b.edges), ref); err != nil {
			errs = append(errs, err)
			for _, id := range b.ids {
				if st.Workflows[id] == nil {
					st.Workflows[id] = &wfMemoryState{ProfileID: pid}
				}
				st.Workflows[id].Pending = true
			}
			continue
		}
		for _, c := range b.commits {
			c()
		}
	}
	return errors.Join(errs...)
}

type wfUpdate struct {
	profileID string
	nodes     []KGNode
	edges     []KGEdge
	commit    func()
}

// evaluate returns the ingest for one workflow, or nil when nothing changed
// or the change is throttled (then it is marked pending for a later sweep).
func (m *RunMemory) evaluate(ctx context.Context, st *runMemState, id string) (*wfUpdate, error) {
	wf, err := m.Source.Workflow(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("run memory: workflow %s: %w", id, err)
	}
	if wf == nil {
		delete(st.Workflows, id)
		return nil, nil
	}
	runs, err := m.Source.RecentRuns(ctx, id, m.window())
	if err != nil {
		return nil, fmt.Errorf("run memory: runs of %s: %w", id, err)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	out := AggregateRuns(runs, m.redact())
	sig := out.Signature()
	ws := st.Workflows[id]
	now := m.now()
	if ws != nil && ws.Signature == sig {
		ws.Pending = false
		return nil, nil
	}
	if ws != nil && now.Sub(ws.LastIngest) < m.interval() {
		ws.Pending = true
		return nil, nil
	}
	pid := wf.ProfileID
	if pid == "" {
		pid = "default"
	}
	var fix *RunFix
	if ws != nil && !ws.LastOK && out.LatestOK && !ws.LastFailAt.IsZero() && wf.UpdatedAt.After(ws.LastFailAt) {
		fix = &RunFix{Class: ws.LastFailClass, FailedAt: ws.LastFailAt, EditedAt: wf.UpdatedAt}
	}
	nodes, edges := BuildRunMemoryGraph(wf, out, fix)
	return &wfUpdate{profileID: pid, nodes: nodes, edges: edges, commit: func() {
		next := &wfMemoryState{ProfileID: pid, Signature: sig, LastIngest: now, LastOK: out.LatestOK}
		if out.LastFailure != nil {
			next.LastFailAt = out.LastFailure.FinishedAt
			next.LastFailClass = out.LastFailure.ErrorClass
		}
		st.Workflows[id] = next
	}}, nil
}

func (m *RunMemory) loadState() *runMemState {
	st := &runMemState{}
	var raw string
	if m.DB != nil {
		if err := m.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, runMemoryStateKey).Scan(&raw); err == nil {
			_ = json.Unmarshal([]byte(raw), st)
		}
	}
	if st.Workflows == nil {
		st.Workflows = map[string]*wfMemoryState{}
	}
	return st
}

func (m *RunMemory) saveState(st *runMemState) error {
	if m.DB == nil {
		return nil
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = m.DB.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, runMemoryStateKey, string(b))
	if err != nil {
		return fmt.Errorf("run memory: saving state: %w", err)
	}
	return nil
}

func (m *RunMemory) sync() SyncFunc {
	if m.Sync != nil {
		return m.Sync
	}
	return SyncToKnowledgeGraph
}

func (m *RunMemory) redact() func(string) string {
	if m.Redact != nil {
		return m.Redact
	}
	return RedactRunError
}

func (m *RunMemory) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *RunMemory) interval() time.Duration {
	if m.MinInterval > 0 {
		return m.MinInterval
	}
	return DefaultRunMemoryInterval
}

func (m *RunMemory) window() int {
	if m.Window > 0 {
		return m.Window
	}
	return DefaultRunMemoryWindow
}
