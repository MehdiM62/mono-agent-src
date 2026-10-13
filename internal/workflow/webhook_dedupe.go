package workflow

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// webhookDeliveryTTL is how long a delivery ID is remembered. Senders that
// retry (GitHub, Stripe, ...) give up well within a week.
const webhookDeliveryTTL = 7 * 24 * time.Hour

// webhookPruneEvery spaces the deletes of expired delivery IDs.
const webhookPruneEvery = time.Hour

// maxDeliveryIDLen bounds a delivery ID taken from a request header.
const maxDeliveryIDLen = 256

// WebhookDeliveries remembers the delivery IDs a workflow's webhook has
// accepted (the dedupe_header option), so a redelivery does not start a
// second run.
type WebhookDeliveries interface {
	// Claim records id for workflowID. When it was already recorded, claimed
	// is false and executionID is the run the first delivery started ("" while
	// that delivery is still starting it).
	Claim(ctx context.Context, workflowID, id string) (claimed bool, executionID string, err error)
	// Attach stores the run a claimed delivery started.
	Attach(ctx context.Context, workflowID, id, executionID string) error
	// Release forgets a claim whose delivery started no run, so the sender's
	// retry is not taken for a duplicate.
	Release(ctx context.Context, workflowID, id string) error
}

// SQLWebhookDeliveries keeps delivery IDs in the webhook_deliveries table
// (migration 074), so they survive a restart.
type SQLWebhookDeliveries struct {
	db *sql.DB

	mu        sync.Mutex
	lastPrune time.Time
}

// NewSQLWebhookDeliveries returns a delivery store over db.
func NewSQLWebhookDeliveries(db *sql.DB) *SQLWebhookDeliveries {
	return &SQLWebhookDeliveries{db: db}
}

func (s *SQLWebhookDeliveries) Claim(ctx context.Context, workflowID, id string) (bool, string, error) {
	s.pruneIfDue(ctx)
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO webhook_deliveries (workflow_id, delivery_id) VALUES (?, ?)`, workflowID, id)
	if err != nil {
		return false, "", err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, "", err
	} else if n == 1 {
		return true, "", nil
	}
	var execID string
	err = s.db.QueryRowContext(ctx,
		`SELECT execution_id FROM webhook_deliveries WHERE workflow_id = ? AND delivery_id = ?`, workflowID, id).Scan(&execID)
	if err == sql.ErrNoRows {
		// Pruned or released between the two statements: claim again.
		return s.Claim(ctx, workflowID, id)
	}
	return false, execID, err
}

func (s *SQLWebhookDeliveries) Attach(ctx context.Context, workflowID, id, executionID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET execution_id = ? WHERE workflow_id = ? AND delivery_id = ?`, executionID, workflowID, id)
	return err
}

func (s *SQLWebhookDeliveries) Release(ctx context.Context, workflowID, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM webhook_deliveries WHERE workflow_id = ? AND delivery_id = ?`, workflowID, id)
	return err
}

// pruneIfDue deletes delivery IDs older than webhookDeliveryTTL, at most once
// per webhookPruneEvery. A failed prune is retried on the next due claim.
func (s *SQLWebhookDeliveries) pruneIfDue(ctx context.Context) {
	s.mu.Lock()
	if time.Since(s.lastPrune) < webhookPruneEvery {
		s.mu.Unlock()
		return
	}
	s.lastPrune = time.Now()
	s.mu.Unlock()
	cutoff := time.Now().UTC().Add(-webhookDeliveryTTL).Format("2006-01-02 15:04:05")
	_, _ = s.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE created_at < ?`, cutoff)
}

// MemoryWebhookDeliveries is the in-process delivery store for an engine with
// no database. IDs are forgotten on restart.
type MemoryWebhookDeliveries struct {
	mu   sync.Mutex
	seen map[[2]string]memoryDelivery
	now  func() time.Time
}

type memoryDelivery struct {
	executionID string
	at          time.Time
}

// NewMemoryWebhookDeliveries returns an empty in-process delivery store.
func NewMemoryWebhookDeliveries() *MemoryWebhookDeliveries {
	return &MemoryWebhookDeliveries{seen: make(map[[2]string]memoryDelivery), now: time.Now}
}

func (m *MemoryWebhookDeliveries) Claim(_ context.Context, workflowID, id string) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for k, d := range m.seen {
		if now.Sub(d.at) > webhookDeliveryTTL {
			delete(m.seen, k)
		}
	}
	key := [2]string{workflowID, id}
	if d, ok := m.seen[key]; ok {
		return false, d.executionID, nil
	}
	m.seen[key] = memoryDelivery{at: now}
	return true, "", nil
}

func (m *MemoryWebhookDeliveries) Attach(_ context.Context, workflowID, id, executionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [2]string{workflowID, id}
	if d, ok := m.seen[key]; ok {
		d.executionID = executionID
		m.seen[key] = d
	}
	return nil
}

func (m *MemoryWebhookDeliveries) Release(_ context.Context, workflowID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.seen, [2]string{workflowID, id})
	return nil
}
