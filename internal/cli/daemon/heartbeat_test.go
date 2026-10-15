package daemon

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/workflow"
)

// The refresh the daemon runs before every heartbeat write carries the account
// verdict: locked and enforced is a real lock, locked and not enforced is the
// warn period (nothing is refused), and a dormant gate says nothing at all.
func TestDaemonHeartbeatRefreshCarriesTheAccount(t *testing.T) {
	engine := workflow.NewWorkflowEngineWithStore(nil, nil, nil, workflow.NewNodeTypeRegistry(),
		workflow.EngineConfig{WebhookAddr: "127.0.0.1:0"}, zerolog.Nop())
	refresh := daemonHeartbeatRefresh(engine)
	var hb daemonhb.Heartbeat

	accounttest.Install(t, accounttest.LockedNoLogin)
	refresh(&hb)
	if hb.Account == nil || hb.Account.State != "locked" || hb.Account.Reason != "not_logged_in" || !hb.Account.Enforced {
		t.Fatalf("locked: Account = %+v, want locked / not_logged_in / enforced", hb.Account)
	}

	account.InstallForTest(t, nil)
	account.SetEnforceFromForTest(t, time.Now().Add(48*time.Hour))
	refresh(&hb)
	if hb.Account == nil || hb.Account.State != "locked" || hb.Account.Reason != "not_logged_in" || hb.Account.Enforced {
		t.Fatalf("warn period: Account = %+v, want locked / not_logged_in / not enforced", hb.Account)
	}

	accounttest.Install(t, accounttest.Dormant)
	refresh(&hb)
	if hb.Account != nil {
		t.Fatalf("dormant: Account = %+v, want nil", hb.Account)
	}
}
