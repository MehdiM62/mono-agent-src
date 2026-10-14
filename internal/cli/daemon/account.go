package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/cli/orgsvc"
	"github.com/monoes/mono-agent/internal/workflow"
)

// The daemon's locked mode (spec D8, section 6.4). While the monoes.me account is
// locked the daemon stays up, because launchd's KeepAlive would respawn an exiting
// one in a loop, and starts nothing: the engine refuses executions and drops
// triggers by itself (internal/workflow), the doors refuse, and this supervisor
// stops the org services. It resumes by itself when a valid session appears: a
// sign-in from the CLI or the app writes session.json and the guard reads it.

// orgStopWait bounds how long the supervisor waits for the org services to end,
// when the account locks and before it starts them again. Their loops end with
// their context; the longest thing in one is an agent turn, killed within seconds.
const orgStopWait = 30 * time.Second

// accountSupervisor starts and stops the org services as the account verdict
// changes, and cancels the running executions whenever the account locks, for any
// reason: nothing runs without a valid sign-in.
// Its dependencies are fields so that a test can drive it.
type accountSupervisor struct {
	status        func() account.Status            // account.CurrentStatus in the daemon
	poll          time.Duration                    // account.PollInterval in the daemon
	startOrg      func(ctx context.Context)        // launches the org services; they end when ctx ends
	waitOrg       func(timeout time.Duration) bool // whether everything startOrg launched has ended
	cancelRunning func(st account.Status) int      // cancels the engine's running executions under this lock
	logf          func(format string, args ...any)
}

func newAccountSupervisor(orgs *orgsvc.Services, engine *workflow.WorkflowEngine) *accountSupervisor {
	return &accountSupervisor{
		status:   account.CurrentStatus,
		poll:     account.PollInterval,
		startOrg: func(ctx context.Context) { orgs.Start(ctx, engine) },
		waitOrg:  orgs.Wait,
		cancelRunning: func(st account.Status) int {
			if st.Reason == account.ReasonRefused {
				return engine.CancelRunning() // records that monoes.me refused the account
			}
			return engine.CancelRunningLocked(string(st.Reason))
		},
		logf: orgs.Logf,
	}
}

// run applies the verdict at once and then every poll, until ctx ends. It never
// returns because of a lock. A daemon that starts locked never starts the org
// services; one that is locked later stops them and starts them again when the
// account is valid. Every lock (a refusal, an expired or removed session, a
// daemon that was never signed in) also cancels what the engine is running, once
// per lock: the user's rule is that nothing runs without a sign-in.
func (s *accountSupervisor) run(ctx context.Context) {
	var (
		stop      context.CancelFunc // non-nil while the org services run
		ran       bool               // the org services ran at some point, so a stop may still be under way
		reported  bool               // this lock has been logged
		cancelled bool               // this lock has cancelled the running executions
	)
	apply := func(st account.Status) {
		if st.Allowed() {
			if reported {
				s.logf("daemon: the monoes.me account is valid again; the org services start again")
			}
			reported, cancelled = false, false
			if stop != nil {
				return
			}
			if ran && !s.waitOrg(orgStopWait) {
				s.logf("daemon: the org services are still stopping; they start again at the next check")
				return
			}
			orgCtx, cancel := context.WithCancel(ctx)
			stop, ran = cancel, true
			s.startOrg(orgCtx)
			return
		}
		// The running executions are cancelled before the wait for the org
		// services, which can last orgStopWait: a locked account runs nothing.
		if !cancelled {
			cancelled = true
			if n := s.cancelRunning(st); n > 0 {
				if st.Reason == account.ReasonRefused {
					s.logf("daemon: monoes.me refused this account: cancelled %d running execution(s)", n)
				} else {
					s.logf("daemon: the monoes.me account is locked (%s): cancelled %d running execution(s)", st.Reason, n)
				}
			}
		}
		if stop != nil {
			stop()
			stop = nil
			s.waitOrg(orgStopWait)
		}
		if !reported {
			reported = true
			s.logf("%s", daemonLockedLine(st))
		}
	}

	apply(s.status())
	t := time.NewTicker(s.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			apply(s.status())
		}
	}
}

// daemonLockedLine is what a locked daemon logs, once per lock: why, and the
// exact command that ends it (spec section 9).
func daemonLockedLine(st account.Status) string {
	fix := "run: monoagentcli account login   (without a browser: monoagentcli account login --email <address>)"
	if st.Reason == account.ReasonKeyUnknown {
		fix = "run: monoagentcli update"
	}
	return fmt.Sprintf("daemon: the monoes.me account is locked (%s): nothing runs and the org services are stopped until a login is valid again; %s", st.Reason, fix)
}

// daemonRunningLine is the line the daemon prints once it is up. A locked one
// says so, since "triggers are live" would be untrue.
func daemonRunningLine(st account.Status) string {
	if st.Allowed() {
		return "Daemon running. Active workflows' triggers are live."
	}
	return "Daemon running, but the monoes.me account is locked (" + string(st.Reason) + "): nothing runs until a login is valid again."
}
