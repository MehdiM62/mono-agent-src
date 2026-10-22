package chat

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/aitask/budget"
	"github.com/monoes/mono-agent/internal/applyfill"
	"github.com/monoes/mono-agent/internal/browserctl"
)

// taskTurn is the AI-task side of a turn in a coder conversation. When the
// conversation has an open task (running, or waiting for the user after its
// turn ended) and the caller is the operator, the turn is that task's next
// turn: it gets a fresh token (the old one stops working) bound to this
// process, in the environment as aitask.EnvTaskID and aitask.EnvToken, and the
// task goes back to running. end records how the turn ended: without
// `aitask complete` the task needs the user again.
//
// An agent, the chat's runs and workflow nodes (anything RequireOperator
// refuses) get nothing; neither does a conversation with no open task, a
// queued task (`aitask start` begins it) or another profile's. The first turn,
// which `aitask supervise` runs with the task's token already in its
// environment, keeps that token.
//
// kind is the browser-permission kind the turn runs as: apply for the turn of
// an open application-apply task (the first turn and every follow-up), read
// only from the task row of this profile once the caller is the operator,
// else coder. Nothing an agent can set (a flag, the environment) picks it.
// An apply turn also comes back confined (chat_apply.go): it is never a
// full-access coder turn, so a task that cannot be confined (no snapshot of
// its context, no rebind) is an error, never a coder turn.
func taskTurn(cfg *Deps, conversationID string, turnCap *float64) (run taskRun) {
	run.end = func(error) {}
	run.kind = browserctl.KindCoder
	if cfg.RequireOperator == nil || cfg.RequireOperator("continue an AI task's turn") != nil {
		return run
	}
	db, err := cfg.OpenDB()
	if err != nil {
		return run
	}
	defer db.Close()
	store := &aitask.Store{DB: db.DB, Profile: cfg.ProfileID()}
	t, ok, err := store.OpenForConversation(conversationID)
	if err != nil {
		run.err = fmt.Errorf("looking up the conversation's task: %w", err)
		return run
	}
	if !ok {
		return run
	}
	apply := t.Kind == applyfill.Kind
	if apply {
		run.kind = browserctl.KindApply
		if run.confine, err = confineApplyTurn(cfg, t.ID); err != nil {
			run.kind, run.err = browserctl.KindCoder, err
			return run
		}
	}
	run.end = func(turnErr error) {
		text := ""
		if turnErr != nil {
			text = "the agent's turn ended with an error: " + turnErr.Error()
		}
		if db, err := cfg.OpenDB(); err == nil {
			defer db.Close()
			_ = (&aitask.Service{Store: &aitask.Store{DB: db.DB, Profile: cfg.ProfileID()}}).EndTurn(t.ID, text)
			// A follow-up turn's spend counts against the same caps as the first.
			l := taskLedger(db.DB)
			_ = l.Settle(context.Background(), t.ID, t.ConversationID)
		}
	}
	if os.Getenv(aitask.EnvTaskID) == t.ID && store.CheckToken(t.ID, os.Getenv(aitask.EnvToken)) {
		return run
	}
	// A follow-up is admitted again, against what is left of the task's cap and
	// the turn's own, before it gets a token: a task with nothing left gets no turn.
	l := taskLedger(db.DB)
	asked := 0.0
	if turnCap != nil {
		asked = *turnCap
	}
	grant, err := l.AdmitTurn(context.Background(), budget.Admission{TaskID: t.ID, ConversationID: t.ConversationID, Kind: t.Kind, Runtime: t.Runtime}, asked)
	if err != nil {
		run.kind, run.confine, run.end, run.err = browserctl.KindCoder, nil, func(error) {}, err
		return run
	}
	run.budget = &grant
	token := aitask.NewToken()
	if moved, err := store.Rebind(t.ID, token, os.Getpid()); err != nil || !moved {
		_ = l.Settle(context.Background(), t.ID, t.ConversationID)
		run.budget = nil
		if apply {
			run.err = fmt.Errorf("task %s could not be handed to this turn", t.ID)
		}
		run.kind, run.confine, run.end = browserctl.KindCoder, nil, func(error) {}
		return run
	}
	run.env = map[string]string{aitask.EnvTaskID: t.ID, aitask.EnvToken: token}
	return run
}

// taskRun is what taskTurn found for a turn.
type taskRun struct {
	env     map[string]string
	kind    string // the browser-permission kind
	confine *confinement
	end     func(error)
	err     error
	// budget is the cap the turn runs under, set for a follow-up turn.
	budget *float64
}

// taskLedger is the budget ledger over db, bounded by the coder budget.
func taskLedger(db *sql.DB) *budget.Ledger {
	l := &budget.Ledger{DB: db}
	if st, err := loadCoderSettings(db); err == nil {
		l.Operator = st.BudgetUSD
	}
	return l
}
