package hilsend

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/publication"
)

// Attribute credits publications recorded by an approved send to the role or
// agent that prepared it. The approved process is the operator's, so without
// this the publication history would not say who wrote the text. It does
// nothing outside an approved send.
func Attribute(ctx context.Context, db *sql.DB, profile string, entries []publication.Entry) {
	id := env(EnvApprovalID)
	if id == "" || len(entries) == 0 {
		return
	}
	if profile == "" {
		profile = "default"
	}
	p, err := (Store{DB: db, Profile: profile}).Get(ctx, id)
	if err != nil {
		return
	}
	who := p.PreparedBy
	agent := who.AgentID
	if agent == "" && who.ConversationID != "" {
		agent = "coder:" + who.ConversationID
	}
	for i := range entries {
		entries[i].AgentID, entries[i].RoleID, entries[i].OrgID = agent, who.Role, who.Org
	}
}
