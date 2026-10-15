package workflow

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// ---------------------------------------------------------------------------
// Executions
// ---------------------------------------------------------------------------

// CreateExecution inserts a new workflow execution record.
// If e.ID is empty a UUID is generated. e.PID is the owner: the pid of the
// process that will run the row, or 0 for a row left for any live engine to
// adopt (see ListAdoptableExecutions).
func (s *SQLiteWorkflowStore) CreateExecution(ctx context.Context, e *WorkflowExecution) error {
	if e.ID == "" {
		e.ID = newID()
	}
	e.CreatedAt = time.Now().UTC()
	if e.Status == "" {
		e.Status = "QUEUED"
	}

	if err := e.MarshalTriggerData(); err != nil {
		return fmt.Errorf("marshalling trigger data for execution %s: %w", e.ID, err)
	}

	profileID := e.ProfileID
	if profileID == "" {
		profileID = "default"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_executions
			(id, workflow_id, status, trigger_type, trigger_data, started_at, finished_at, error_message, created_at, profile_id, pid, trigger_node_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.WorkflowID, e.Status, e.TriggerType, e.TriggerDataRaw,
		e.StartedAt, e.FinishedAt, e.ErrorMessage, e.CreatedAt, profileID, e.PID, e.TriggerNodeID,
	)
	if err != nil {
		return fmt.Errorf("creating execution %s: %w", e.ID, err)
	}
	return nil
}

// GetExecution retrieves a single execution by ID, populating its Nodes slice.
// Returns nil, nil when not found.
func (s *SQLiteWorkflowStore) GetExecution(ctx context.Context, id string) (*WorkflowExecution, error) {
	e := &WorkflowExecution{}
	var createdAt sqliteTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workflow_id, status, trigger_type, trigger_data, started_at, finished_at, COALESCE(error_message,''), created_at, COALESCE(resume_state,''), COALESCE(profile_id,'default'), COALESCE(trigger_node_id,''), COALESCE(error_class,'')
		FROM workflow_executions WHERE id = ?`, id,
	).Scan(
		&e.ID, &e.WorkflowID, &e.Status, &e.TriggerType, &e.TriggerDataRaw,
		newSqliteNullTime(&e.StartedAt), newSqliteNullTime(&e.FinishedAt), &e.ErrorMessage, &createdAt, &e.ResumeState, &e.ProfileID, &e.TriggerNodeID, &e.ErrorClass,
	)
	e.CreatedAt = createdAt.Time
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting execution %s: %w", id, err)
	}

	if err := e.ParseTriggerData(); err != nil {
		return nil, fmt.Errorf("parsing trigger data for execution %s: %w", id, err)
	}

	nodes, err := s.loadExecutionNodes(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Nodes = nodes

	return e, nil
}

// ListExecutions returns executions for a workflow ordered by created_at DESC.
// Pass limit <= 0 to return all executions. Execution nodes are not populated.
func (s *SQLiteWorkflowStore) ListExecutions(ctx context.Context, workflowID string, limit int) ([]WorkflowExecution, error) {
	query := `
		SELECT id, workflow_id, status, trigger_type, trigger_data, started_at, finished_at, COALESCE(error_message,''), created_at, COALESCE(error_class,'')
		FROM workflow_executions WHERE workflow_id = ? ORDER BY created_at DESC`
	var args []interface{}
	args = append(args, workflowID)

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing executions for workflow %s: %w", workflowID, err)
	}
	defer rows.Close()

	var out []WorkflowExecution
	for rows.Next() {
		e := WorkflowExecution{}
		var createdAt sqliteTime
		if err := rows.Scan(
			&e.ID, &e.WorkflowID, &e.Status, &e.TriggerType, &e.TriggerDataRaw,
			newSqliteNullTime(&e.StartedAt), newSqliteNullTime(&e.FinishedAt), &e.ErrorMessage, &createdAt, &e.ErrorClass,
		); err != nil {
			return nil, fmt.Errorf("scanning execution row: %w", err)
		}
		e.CreatedAt = createdAt.Time
		if err := e.ParseTriggerData(); err != nil {
			return nil, fmt.Errorf("parsing trigger data for execution %s: %w", e.ID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateExecutionStatus sets status and error_message on an execution.
func (s *SQLiteWorkflowStore) UpdateExecutionStatus(ctx context.Context, id string, status string, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE workflow_executions SET status = ?, error_message = ? WHERE id = ?",
		status, errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("updating execution status %s: %w", id, err)
	}
	return nil
}

// StartQueuedExecution is the guarded QUEUED → RUNNING transition every
// dispatch goes through: it records started_at and this process's pid only
// when the row is still QUEUED and owned by this process (or unowned). It
// returns false when another dispatch, engine or a cancel got there first —
// the caller must then not run the execution, so no body ever runs twice.
// The resume path qualifies too: ResumeWaitingExecution leaves the row
// QUEUED and owned by the resuming process.
func (s *SQLiteWorkflowStore) StartQueuedExecution(ctx context.Context, id string) (bool, error) {
	self := os.Getpid()
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET status = 'RUNNING', started_at = ?, pid = ?
		 WHERE id = ? AND status = 'QUEUED' AND COALESCE(pid, 0) IN (0, ?)`,
		time.Now().UTC(), self, id, self)
	if err != nil {
		return false, fmt.Errorf("starting queued execution %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetExecutionStarted marks an execution as RUNNING and records started_at and the current PID.
func (s *SQLiteWorkflowStore) SetExecutionStarted(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		"UPDATE workflow_executions SET status = 'RUNNING', started_at = ?, pid = ? WHERE id = ?",
		now, os.Getpid(), id,
	)
	if err != nil {
		return fmt.Errorf("setting execution started %s: %w", id, err)
	}
	return nil
}

// SetExecutionFinished records finished_at, final status and optional error.
// It also clears any resume_state — a finished execution is never resumed.
// A row already CANCELLED (`workflow cancel` from another process) stays
// CANCELLED whatever the run ended with.
func (s *SQLiteWorkflowStore) SetExecutionFinished(ctx context.Context, id string, status string, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		"UPDATE workflow_executions SET status = ?, finished_at = ?, error_message = ?, resume_state = '' WHERE id = ? AND (status <> 'CANCELLED' OR ? = 'CANCELLED')",
		status, now, errMsg, id, status,
	)
	if err != nil {
		return fmt.Errorf("setting execution finished %s: %w", id, err)
	}
	return nil
}

// SetExecutionWaiting marks an execution as paused (WAITING) and stores the
// serialized in-flight state needed to resume it. Unlike SetExecutionFinished,
// it does not set finished_at — the execution is suspended, not done.
func (s *SQLiteWorkflowStore) SetExecutionWaiting(ctx context.Context, id string, resumeState string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE workflow_executions SET status = 'WAITING', resume_state = ? WHERE id = ?",
		resumeState, id,
	)
	if err != nil {
		return fmt.Errorf("setting execution waiting %s: %w", id, err)
	}
	return nil
}

// ListResumableExecutions returns the IDs of WAITING executions that have no
// still-pending Human-in-Loop items — i.e. every pause point has been resolved
// (approved or rejected), so the execution can be resumed — and whose
// resume_after poll window (PauseError) has passed. Event-woken pauses are
// resumed directly by ResumeExecution and never wait for the window.
func (s *SQLiteWorkflowStore) ListResumableExecutions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM workflow_executions
		WHERE status = 'WAITING'
		  AND (resume_after IS NULL OR resume_after <= ?)
		  AND id NOT IN (SELECT execution_id FROM hil_pending WHERE status = 'pending')`,
		formatResumeAfter(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("listing resumable executions: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning resumable execution: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---------------------------------------------------------------------------
// Execution nodes
// ---------------------------------------------------------------------------

// CreateExecutionNode inserts a new execution-node record.
// If en.ID is empty a UUID is generated.
func (s *SQLiteWorkflowStore) CreateExecutionNode(ctx context.Context, en *WorkflowExecutionNode) error {
	if en.ID == "" {
		en.ID = newID()
	}
	if en.Status == "" {
		en.Status = "PENDING"
	}

	if err := en.MarshalItems(); err != nil {
		return fmt.Errorf("marshalling items for execution node %s: %w", en.ID, err)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_execution_nodes
			(id, execution_id, node_id, node_name, status, input_items, output_items, error_message, started_at, finished_at, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		en.ID, en.ExecutionID, en.NodeID, en.NodeName, en.Status,
		en.InputRaw, en.OutputRaw, en.ErrorMessage,
		en.StartedAt, en.FinishedAt, en.RetryCount,
	)
	if err != nil {
		return fmt.Errorf("creating execution node %s: %w", en.ID, err)
	}
	return nil
}

// UpdateExecutionNode updates all mutable fields of an execution-node record.
func (s *SQLiteWorkflowStore) UpdateExecutionNode(ctx context.Context, en *WorkflowExecutionNode) error {
	if err := en.MarshalItems(); err != nil {
		return fmt.Errorf("marshalling items for execution node %s: %w", en.ID, err)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE workflow_execution_nodes
		SET status = ?, input_items = ?, output_items = ?, error_message = ?,
		    started_at = ?, finished_at = ?, retry_count = ?
		WHERE id = ?`,
		en.Status, en.InputRaw, en.OutputRaw, en.ErrorMessage,
		en.StartedAt, en.FinishedAt, en.RetryCount, en.ID,
	)
	if err != nil {
		return fmt.Errorf("updating execution node %s: %w", en.ID, err)
	}
	return nil
}

// SetExecutionNodeFinished marks an execution node as finished, marshals
// outputItems to JSON, and records status, error_message, and finished_at.
func (s *SQLiteWorkflowStore) SetExecutionNodeFinished(ctx context.Context, id string, status string, outputItems []Item, errMsg string) error {
	outputRaw, err := marshalItems(outputItems)
	if err != nil {
		return fmt.Errorf("marshalling output items for execution node %s: %w", id, err)
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		UPDATE workflow_execution_nodes
		SET status = ?, output_items = ?, error_message = ?, finished_at = ?
		WHERE id = ?`,
		status, outputRaw, errMsg, now, id,
	)
	if err != nil {
		return fmt.Errorf("setting execution node finished %s: %w", id, err)
	}
	return nil
}

// loadExecutionNodes is an internal helper to fetch execution-node records.
func (s *SQLiteWorkflowStore) loadExecutionNodes(ctx context.Context, executionID string) ([]WorkflowExecutionNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, execution_id, node_id, node_name, status, input_items, output_items, error_message, started_at, finished_at, retry_count,
		       COALESCE(error_class,''), COALESCE(error_hint,'')
		FROM workflow_execution_nodes WHERE execution_id = ? ORDER BY rowid ASC`, executionID)
	if err != nil {
		return nil, fmt.Errorf("loading execution nodes for execution %s: %w", executionID, err)
	}
	defer rows.Close()

	var out []WorkflowExecutionNode
	for rows.Next() {
		en := WorkflowExecutionNode{}
		if err := rows.Scan(
			&en.ID, &en.ExecutionID, &en.NodeID, &en.NodeName, &en.Status,
			&en.InputRaw, &en.OutputRaw, &en.ErrorMessage,
			newSqliteNullTime(&en.StartedAt), newSqliteNullTime(&en.FinishedAt), &en.RetryCount,
			&en.ErrorClass, &en.ErrorHint,
		); err != nil {
			return nil, fmt.Errorf("scanning execution node row: %w", err)
		}
		if err := en.ParseItems(); err != nil {
			return nil, fmt.Errorf("parsing items for execution node %s: %w", en.ID, err)
		}
		out = append(out, en)
	}
	return out, rows.Err()
}
