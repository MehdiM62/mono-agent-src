// Package agentusage records the token and cost use of agent turns, one row
// per turn in the agent_usage table (migration 080). agent.ask records every
// turn it runs; chat and other callers can record theirs with Record or
// RecordTurn.
package agentusage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/monomind"
)

// Sources of a usage row.
const (
	SourceAgentAsk = "agent.ask"
	SourceChat     = "chat"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// Turn is one agent turn's usage. A nil token or cost field means the
// runtime did not report it.
type Turn struct {
	At          time.Time `json:"at"`
	ProfileID   string    `json:"profile_id"`
	Source      string    `json:"source"`
	WorkflowID  string    `json:"workflow_id,omitempty"`
	ExecutionID string    `json:"execution_id,omitempty"`
	NodeID      string    `json:"node_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Runtime     string    `json:"runtime"`
	Model       string    `json:"model,omitempty"`
	InputTokens *int64    `json:"input_tokens,omitempty"`
	// OutputTokens and CostUSD: see InputTokens.
	OutputTokens *int64   `json:"output_tokens,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty"`
}

// FromResult fills t's session, tokens and cost from what res reported.
func (t Turn) FromResult(res *monomind.TurnResult) Turn {
	if res == nil {
		return t
	}
	if t.SessionID == "" {
		t.SessionID = res.SessionID
	}
	if res.HasInputTokens {
		v := res.InputTokens
		t.InputTokens = &v
	}
	if res.HasOutputTokens {
		v := res.OutputTokens
		t.OutputTokens = &v
	}
	if res.HasCostUSD {
		v := res.CostUSD
		t.CostUSD = &v
	}
	return t
}

// Record stores t. A zero At is now; an empty ProfileID is "default".
func Record(ctx context.Context, db *sql.DB, t Turn) error {
	if db == nil {
		return nil
	}
	if t.At.IsZero() {
		t.At = time.Now()
	}
	if t.ProfileID == "" {
		t.ProfileID = "default"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO agent_usage
		(at, profile_id, source, workflow_id, execution_id, node_id, session_id, runtime, model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.At.UTC().Format(timeLayout), t.ProfileID, t.Source, t.WorkflowID, t.ExecutionID, t.NodeID, t.SessionID,
		t.Runtime, t.Model, nullInt(t.InputTokens), nullInt(t.OutputTokens), nullFloat(t.CostUSD))
	if err != nil {
		return fmt.Errorf("recording agent usage: %w", err)
	}
	return nil
}

// RecordTurn stores t with res's usage: the call chat turns can make.
func RecordTurn(ctx context.Context, db *sql.DB, t Turn, res *monomind.TurnResult) error {
	return Record(ctx, db, t.FromResult(res))
}

// List returns the turns recorded at or after since, oldest first.
func List(ctx context.Context, db *sql.DB, since time.Time) ([]Turn, error) {
	rows, err := db.QueryContext(ctx, `SELECT at, profile_id, source, workflow_id, execution_id, node_id, session_id,
		runtime, model, input_tokens, output_tokens, cost_usd
		FROM agent_usage WHERE at >= ? ORDER BY at, id`, since.UTC().Format(timeLayout))
	if err != nil {
		return nil, fmt.Errorf("listing agent usage: %w", err)
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		var (
			t       Turn
			at      string
			in, ou  sql.NullInt64
			costUSD sql.NullFloat64
		)
		if err := rows.Scan(&at, &t.ProfileID, &t.Source, &t.WorkflowID, &t.ExecutionID, &t.NodeID, &t.SessionID,
			&t.Runtime, &t.Model, &in, &ou, &costUSD); err != nil {
			return nil, fmt.Errorf("listing agent usage: %w", err)
		}
		t.At, _ = time.Parse(timeLayout, at)
		if in.Valid {
			t.InputTokens = &in.Int64
		}
		if ou.Valid {
			t.OutputTokens = &ou.Int64
		}
		if costUSD.Valid {
			t.CostUSD = &costUSD.Float64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
