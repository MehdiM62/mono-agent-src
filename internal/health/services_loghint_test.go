package health

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/autostart"
)

// The daemon's log is where it was started from: the login service's log
// (the user journal on Linux) when one is registered, else the file the
// detached start writes.
func TestDaemonLogHintFollowsHowTheDaemonStarts(t *testing.T) {
	ctx := context.Background()
	registered := false
	env := &Env{AutostartStatus: func(context.Context) (bool, string) { return registered, "unit" }}

	if got, want := daemonLogHint(ctx, env), filepath.Join(".monoagent", "logs", "daemon.log"); !strings.HasSuffix(got, want) {
		t.Errorf("no service: hint %q, want the detached start's %s", got, want)
	}
	if got := daemonLogHint(ctx, &Env{}); !strings.HasSuffix(got, "daemon.log") {
		t.Errorf("no autostart probe: hint %q", got)
	}

	registered = true
	got := daemonLogHint(ctx, env)
	if got != autostart.ServiceLogHint() {
		t.Errorf("service: hint %q, want %q", got, autostart.ServiceLogHint())
	}
	if runtime.GOOS == "linux" && got != "journalctl --user -u monoagent-daemon.service" {
		t.Errorf("linux service: hint %q, want the journal of the systemd unit", got)
	}
}

func TestBridgeDownPointsAtTheDaemonsLog(t *testing.T) {
	ctx := context.Background()
	prev := serviceLogHint
	serviceLogHint = func() string { return "journalctl --user -u x.service" }
	t.Cleanup(func() { serviceLogHint = prev })
	env := &Env{
		Daemon:          func(context.Context) DaemonInfo { return DaemonInfo{Running: true, PID: 7} },
		AutostartStatus: func(context.Context) (bool, string) { return true, "unit" },
	}
	if res := bridgeDown(ctx, env); !strings.Contains(res.Detail, "(see journalctl --user -u x.service)") {
		t.Errorf("detail %q does not point at the service's log", res.Detail)
	}
}
