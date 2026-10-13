package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// newID generates a new UUID string for use as a primary key.
func newID() string {
	return uuid.New().String()
}

// sqliteTime wraps time.Time so it can scan both time.Time and string values
// from SQLite (modernc.org/sqlite stores timestamps as text).
type sqliteTime struct{ time.Time }

func (st *sqliteTime) Scan(src interface{}) error {
	switch v := src.(type) {
	case time.Time:
		st.Time = v
		return nil
	case string:
		for _, layout := range []string{
			time.RFC3339Nano, time.RFC3339,
			"2006-01-02T15:04:05Z", "2006-01-02 15:04:05",
			"2006-01-02 15:04:05.999999999 -0700 MST",
			"2006-01-02 15:04:05.999999999 +0000 UTC",
		} {
			if t, err := time.Parse(layout, v); err == nil {
				st.Time = t
				return nil
			}
		}
		return fmt.Errorf("sqliteTime: cannot parse %q", v)
	case nil:
		st.Time = time.Time{}
		return nil
	default:
		return fmt.Errorf("sqliteTime: unsupported type %T", src)
	}
}

// sqliteNullTime wraps *time.Time for nullable timestamp columns.
type sqliteNullTime struct{ ptr **time.Time }

func newSqliteNullTime(dst **time.Time) sqliteNullTime { return sqliteNullTime{ptr: dst} }

func (sn sqliteNullTime) Scan(src interface{}) error {
	if src == nil {
		*sn.ptr = nil
		return nil
	}
	var st sqliteTime
	if err := st.Scan(src); err != nil {
		return err
	}
	t := st.Time
	*sn.ptr = &t
	return nil
}

// ---------------------------------------------------------------------------
// WorkflowStore interface
// ---------------------------------------------------------------------------

// WorkflowStore defines all persistence operations for the workflow system.
type WorkflowStore interface {
	// Workflows
	CreateWorkflow(ctx context.Context, w *Workflow) error
	GetWorkflow(ctx context.Context, id string) (*Workflow, error)
	ListWorkflows(ctx context.Context, profileID string) ([]Workflow, error)
	UpdateWorkflow(ctx context.Context, w *Workflow) error
	DeleteWorkflow(ctx context.Context, id string) error
	SetWorkflowActive(ctx context.Context, id string, active bool) error

	// Nodes (upsert all nodes for a workflow — delete removed, insert/update existing)
	SaveWorkflowNodes(ctx context.Context, workflowID string, nodes []WorkflowNode) error
	// Connections (upsert all connections for a workflow — delete removed, insert/update existing)
	SaveWorkflowConnections(ctx context.Context, workflowID string, conns []WorkflowConnection) error

	// Executions
	CreateExecution(ctx context.Context, e *WorkflowExecution) error
	GetExecution(ctx context.Context, id string) (*WorkflowExecution, error)
	GetExecutionStatus(ctx context.Context, id string) (string, error)
	ListExecutions(ctx context.Context, workflowID string, limit int) ([]WorkflowExecution, error)
	UpdateExecutionStatus(ctx context.Context, id string, status string, errMsg string) error
	SetExecutionStarted(ctx context.Context, id string) error
	SetExecutionFinished(ctx context.Context, id string, status string, errMsg string) error
	SetExecutionWaiting(ctx context.Context, id string, resumeState string) error
	ResumeWaitingExecution(ctx context.Context, id string) (bool, error)
	ListResumableExecutions(ctx context.Context) ([]string, error)

	// Adoption: unowned QUEUED executions (pid 0, no resume state) are
	// claimed and run by whichever engine is alive.
	ListAdoptableExecutions(ctx context.Context) ([]string, error)
	ClaimQueuedExecution(ctx context.Context, id string) (bool, error)

	// Execution nodes
	CreateExecutionNode(ctx context.Context, en *WorkflowExecutionNode) error
	UpdateExecutionNode(ctx context.Context, en *WorkflowExecutionNode) error
	SetExecutionNodeFinished(ctx context.Context, id string, status string, outputItems []Item, errMsg string) error

	// Recovery
	RecoverStaleExecutions(ctx context.Context) error
	ReapStaleRunningExecutions(ctx context.Context, olderThan time.Time) error
	CancelQueuedExecution(ctx context.Context, id string) (bool, error)
	PruneExecutions(ctx context.Context, workflowID string, keepCount int) error

	// RawDB returns the underlying *sql.DB for use by subsystems that need
	// direct DB access (e.g., vault registration).
	RawDB() *sql.DB
}

// ---------------------------------------------------------------------------
// SQLiteWorkflowStore
// ---------------------------------------------------------------------------

// SQLiteWorkflowStore implements WorkflowStore using a *sql.DB (SQLite).
type SQLiteWorkflowStore struct {
	db *sql.DB
}

// NewSQLiteWorkflowStore creates a new SQLiteWorkflowStore backed by db.
// The store does not run migrations; use the existing migration system.
func NewSQLiteWorkflowStore(db *sql.DB) *SQLiteWorkflowStore {
	return &SQLiteWorkflowStore{db: db}
}

// RawDB returns the underlying *sql.DB.
func (s *SQLiteWorkflowStore) RawDB() *sql.DB { return s.db }

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// boolToInt converts a Go bool to a SQLite-friendly integer (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalItems encodes a slice of Items to a JSON string.
// Returns "[]" for a nil or empty slice.
func marshalItems(items []Item) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
