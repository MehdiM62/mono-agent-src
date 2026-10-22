package chat

import (
	"os"

	"github.com/monoes/mono-agent/internal/aitask"
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
func taskTurn(cfg *Deps, conversationID string) (env map[string]string, end func(error)) {
	noop := func(error) {}
	if cfg.RequireOperator == nil || cfg.RequireOperator("continue an AI task's turn") != nil {
		return nil, noop
	}
	db, err := cfg.OpenDB()
	if err != nil {
		return nil, noop
	}
	defer db.Close()
	store := &aitask.Store{DB: db.DB, Profile: cfg.ProfileID()}
	t, ok, err := store.OpenForConversation(conversationID)
	if err != nil || !ok {
		return nil, noop
	}
	end = func(turnErr error) {
		text := ""
		if turnErr != nil {
			text = "the agent's turn ended with an error: " + turnErr.Error()
		}
		if db, err := cfg.OpenDB(); err == nil {
			defer db.Close()
			_ = (&aitask.Service{Store: &aitask.Store{DB: db.DB, Profile: cfg.ProfileID()}}).EndTurn(t.ID, text)
		}
	}
	if os.Getenv(aitask.EnvTaskID) == t.ID && store.CheckToken(t.ID, os.Getenv(aitask.EnvToken)) {
		return nil, end
	}
	token := aitask.NewToken()
	if moved, err := store.Rebind(t.ID, token, os.Getpid()); err != nil || !moved {
		return nil, noop
	}
	return map[string]string{aitask.EnvTaskID: t.ID, aitask.EnvToken: token}, end
}
