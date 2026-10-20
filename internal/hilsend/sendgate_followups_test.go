package hilsend

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
)

// A break character inside an address's local part must not leave its tail
// to be read as the allowed recipient (#917 N1b): the send gets its card.
func TestABreakCharInsideALocalPartIsNotReadAsItsTail(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	st := allowIn(t, db, "comm.email_send", emailConfig())
	if h, _ := held(t, st, "comm.email_send", emailConfig()); h {
		t.Fatal("the allowed recipient alone was held")
	}
	for _, x := range []string{"evil|ada@example.test", "evil'ada@example.test", `"evil ada"@example.test`, "evil(ada)@example.test"} {
		cfg := with(emailConfig(), "note", x)
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%q: went out without a card", x)
		}
	}
}

// The time exemption does not cover a recipient-like key: a US number whose
// area code starts 2, 3 or 4 reads as an epoch (#917 N4b).
func TestATimeExemptionDoesNotCoverARecipientLikeKey(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	st := allowIn(t, db, "comm.email_send", emailConfig())
	if h, _ := held(t, st, "comm.email_send", with(emailConfig(), "send_at", "1700000000")); h {
		t.Error("an epoch under send_at was held")
	}
	if h, _ := held(t, st, "comm.email_send", with(emailConfig(), "phone_start", "2025550123")); !h {
		t.Error("a US number under a recipient-like time key went out without a card")
	}
}

// A saved connection's server includes its port (#913).
func TestAnotherPortOfTheConnectionsServerIsNotItsOwn(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	c := &connections.Connection{Platform: "smtp", Method: connections.MethodAppPass, ProfileID: "p1",
		Data: map[string]interface{}{"smtp_host": "smtp.example.test", "smtp_port": float64(587), "password": "hunter2"}}
	if err := connections.NewStore(db).Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	merged := func(port interface{}) map[string]interface{} {
		return with(emailConfig(), "credential_id", c.ID, "smtp_host", "smtp.example.test", "smtp_port", port,
			"credential", map[string]interface{}{"smtp_host": "smtp.example.test", "password": "hunter2"})
	}
	st := allowIn(t, db, "comm.email_send", merged(float64(587)))
	if h, _ := held(t, st, "comm.email_send", merged("587")); h {
		t.Error("the connection's own port was held")
	}
	if h, _ := held(t, st, "comm.email_send", merged(float64(2525))); !h {
		t.Error("another port went out without a card")
	}
}

// Mattermost's teamId and Zammad's group are destinations (#917).
func TestMattermostTeamAndZammadGroupAreDestinationKeys(t *testing.T) {
	if !JudgedByKey("app.mattermost", "teamId") || !JudgedByKey("app.zammad", "group") {
		t.Error("teamId / group are not judged by name")
	}
}
