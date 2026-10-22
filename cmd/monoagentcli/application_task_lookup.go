package main

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/cli/application"
)

// `application show --assets` names the application's coder task from the
// aitask table; internal/cli/application cannot import aitask itself.
func init() {
	application.LookupAITask = func(_ context.Context, db *sql.DB, profileID, applicationID string) (*application.AssetAITask, error) {
		t, err := (&aitask.Store{DB: db, Profile: profileID}).ForApplication(applicationID)
		if err != nil || t == nil {
			return nil, err
		}
		return &application.AssetAITask{TaskID: t.ID, ConversationID: t.ConversationID, Status: t.Status}, nil
	}
}
