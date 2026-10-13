package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/vault"
)

// State that outlives a single execution: "seen" dedupe values and key/value
// static data, both stored in SQLite (migration 073) and keyed by workflow id
// plus node id. A node reaches them from Execute through the DB the engine
// puts in ctx and the ids in its NodeInput:
//
//	seen, err := workflow.SeenKeysFor(ctx, input, workflow.StateScopeNode, workflow.SeenOptions{})
//	fresh, err := seen.Check(ctx, ids) // fresh[i]: ids[i] was not seen by an earlier run
//
//	sd, err := workflow.StaticDataFor(ctx, input, workflow.StateScopeNode)
//	ok, err := sd.Get(ctx, "cursor", &cur) ... sd.Set(ctx, "cursor", cur)
//
// The rows are removed when the workflow is deleted (DeleteWorkflowState).

// StateScope selects whose state a handle reads and writes.
type StateScope string

const (
	// StateScopeNode is private to one node of one workflow.
	StateScopeNode StateScope = "node"
	// StateScopeWorkflow is shared by every node of one workflow.
	StateScopeWorkflow StateScope = "workflow"
)

// DefaultSeenHistorySize bounds how many values a SeenKeys scope keeps when
// SeenOptions.HistorySize is 0.
const DefaultSeenHistorySize = 10000

// ErrNoWorkflowState is returned when a run has no database in ctx or lacks
// the workflow (or, for node scope, node) id the state is keyed by.
var ErrNoWorkflowState = errors.New("workflow state unavailable: this run has no database or workflow/node id")

// stateKey resolves the (db, workflow id, node id) a handle works on.
func stateKey(ctx context.Context, input NodeInput, scope StateScope) (*sql.DB, string, string, error) {
	db := vault.DBFromContext(ctx)
	if db == nil || input.WorkflowID == "" {
		return nil, "", "", ErrNoWorkflowState
	}
	switch scope {
	case StateScopeNode:
		if input.NodeID == "" {
			return nil, "", "", ErrNoWorkflowState
		}
		return db, input.WorkflowID, input.NodeID, nil
	case StateScopeWorkflow:
		return db, input.WorkflowID, "", nil
	default:
		return nil, "", "", fmt.Errorf("workflow state: unknown scope %q (want %q or %q)", scope, StateScopeNode, StateScopeWorkflow)
	}
}

// SeenOptions bounds a SeenKeys history.
type SeenOptions struct {
	// HistorySize is the most values kept, the least recently seen dropped
	// first. 0 means DefaultSeenHistorySize; negative means no bound.
	HistorySize int
	// TTL forgets a value not seen for this long. 0 means never.
	TTL time.Duration
}

// SeenKeys is a persisted set of dedupe values for one scope.
type SeenKeys struct {
	db         *sql.DB
	workflowID string
	nodeID     string
	opts       SeenOptions
	now        func() time.Time
}

// SeenKeysFor returns the seen-values history of input's node or workflow.
func SeenKeysFor(ctx context.Context, input NodeInput, scope StateScope, opts SeenOptions) (*SeenKeys, error) {
	db, wf, node, err := stateKey(ctx, input, scope)
	if err != nil {
		return nil, err
	}
	if opts.HistorySize == 0 {
		opts.HistorySize = DefaultSeenHistorySize
	}
	return &SeenKeys{db: db, workflowID: wf, nodeID: node, opts: opts, now: time.Now}, nil
}

func hashSeenValue(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// Check records every value as seen and reports, per value, whether it is new:
// not seen by an earlier call (within the TTL and history size) nor earlier
// in values. Seeing a value again refreshes it, so a value that keeps coming
// back is never the one evicted.
func (s *SeenKeys) Check(ctx context.Context, values []string) ([]bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("seen keys: begin: %w", err)
	}
	defer tx.Rollback()

	now := s.now().UnixNano()
	if s.opts.TTL > 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM workflow_seen_keys WHERE workflow_id = ? AND node_id = ? AND seen_at < ?`,
			s.workflowID, s.nodeID, now-s.opts.TTL.Nanoseconds()); err != nil {
			return nil, fmt.Errorf("seen keys: expire: %w", err)
		}
	}

	fresh := make([]bool, len(values))
	for i, v := range values {
		// Offset by position so values of one batch keep their order for eviction.
		at := now + int64(i)
		h := hashSeenValue(v)
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO workflow_seen_keys (workflow_id, node_id, key_hash, seen_at) VALUES (?, ?, ?, ?)`,
			s.workflowID, s.nodeID, h, at)
		if err != nil {
			return nil, fmt.Errorf("seen keys: insert: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			fresh[i] = true
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE workflow_seen_keys SET seen_at = ? WHERE workflow_id = ? AND node_id = ? AND key_hash = ?`,
			at, s.workflowID, s.nodeID, h); err != nil {
			return nil, fmt.Errorf("seen keys: refresh: %w", err)
		}
	}

	if s.opts.HistorySize > 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM workflow_seen_keys WHERE workflow_id = ? AND node_id = ? AND key_hash NOT IN (
				SELECT key_hash FROM workflow_seen_keys WHERE workflow_id = ? AND node_id = ?
				ORDER BY seen_at DESC LIMIT ?)`,
			s.workflowID, s.nodeID, s.workflowID, s.nodeID, s.opts.HistorySize); err != nil {
			return nil, fmt.Errorf("seen keys: trim: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("seen keys: commit: %w", err)
	}
	return fresh, nil
}

// Clear forgets every value in this scope.
func (s *SeenKeys) Clear(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM workflow_seen_keys WHERE workflow_id = ? AND node_id = ?`, s.workflowID, s.nodeID); err != nil {
		return fmt.Errorf("seen keys: clear: %w", err)
	}
	return nil
}

// StaticData is a persisted key/value store for one scope; values are JSON.
type StaticData struct {
	db         *sql.DB
	workflowID string
	nodeID     string
}

// StaticDataFor returns the static data of input's node or workflow.
func StaticDataFor(ctx context.Context, input NodeInput, scope StateScope) (*StaticData, error) {
	db, wf, node, err := stateKey(ctx, input, scope)
	if err != nil {
		return nil, err
	}
	return &StaticData{db: db, workflowID: wf, nodeID: node}, nil
}

// Get decodes the value stored under key into dst and reports whether there was one.
func (s *StaticData) Get(ctx context.Context, key string, dst interface{}) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM workflow_static_data WHERE workflow_id = ? AND node_id = ? AND key = ?`,
		s.workflowID, s.nodeID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("static data: get %q: %w", key, err)
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return false, fmt.Errorf("static data: decode %q: %w", key, err)
	}
	return true, nil
}

// Set stores value (JSON-encoded) under key, replacing any previous value.
func (s *StaticData) Set(ctx context.Context, key string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("static data: encode %q: %w", key, err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_static_data (workflow_id, node_id, key, value) VALUES (?, ?, ?, ?)
		 ON CONFLICT (workflow_id, node_id, key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		s.workflowID, s.nodeID, key, string(raw)); err != nil {
		return fmt.Errorf("static data: set %q: %w", key, err)
	}
	return nil
}

// Delete removes key; a missing key is not an error.
func (s *StaticData) Delete(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM workflow_static_data WHERE workflow_id = ? AND node_id = ? AND key = ?`,
		s.workflowID, s.nodeID, key); err != nil {
		return fmt.Errorf("static data: delete %q: %w", key, err)
	}
	return nil
}

// DeleteWorkflowState removes every seen value and static data row of a workflow.
func DeleteWorkflowState(ctx context.Context, db *sql.DB, workflowID string) error {
	for _, table := range []string{"workflow_seen_keys", "workflow_static_data"} {
		if _, err := db.ExecContext(ctx, `DELETE FROM `+table+` WHERE workflow_id = ?`, workflowID); err != nil {
			return fmt.Errorf("delete workflow state (%s): %w", table, err)
		}
	}
	return nil
}
