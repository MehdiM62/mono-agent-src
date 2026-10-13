package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/autostart"
)

// daemonDeps is testDeps with a daemon running version (nothing runs for "-"), what the update
// reads from the database (inFlight, settingsErr, err), fake as the service manager with
// servicePID as its main pid, and a short verify wait. hb is the heartbeat a restart rewrites.
func daemonDeps(t *testing.T, latestURL, version string, inFlight int, settingsErr, err error, fake *fakeAutostart, servicePID int, pidErr error) (Deps, *fakeHeartbeat) {
	t.Helper()
	d := testDeps(t, latestURL)
	hb := &fakeHeartbeat{}
	if version != "-" {
		hb.write(os.Getpid(), version)
	}
	d.Heartbeat = hb.read
	d.RestartBlockers = func(context.Context, int) (int, error, error) { return inFlight, settingsErr, err }
	d.Installer = func() autostart.Installer { return fake }
	d.ServicePID = func(context.Context, autostart.Installer) (int, error) { return servicePID, pidErr }
	d.VerifyWait, d.VerifyTick = 300*time.Millisecond, 10*time.Millisecond
	return d, hb
}

// beatsNewVersion makes a restart bring the daemon back with a heartbeat of version.
func beatsNewVersion(hb *fakeHeartbeat, version string) func() {
	return func() { hb.write(os.Getpid(), version) }
}

func TestDaemonAfterUpdate(t *testing.T) {
	t.Parallel()
	damaged := errors.New("the saved settings are damaged")
	for _, c := range []struct {
		name       string
		daemon     string // the version in the heartbeat; "-" means no daemon runs
		inFlight   int
		settings   error
		count      error
		registered bool
		restartErr error
		action     string // "" means nothing to do
		restarted  int
		says       []string
	}{
		{"no daemon runs", "-", 0, nil, nil, true, nil, "", 0, nil},
		{"it already runs the new version", "v0.108.0", 0, nil, nil, true, nil, "", 0, nil},
		{"an old daemon with nothing in flight", "v0.105.0", 0, nil, nil, true, nil, "restarted", 1, []string{"Restarted the daemon", "v0.108.0", "v0.105.0"}},
		{"a heartbeat with no version", "", 0, nil, nil, true, nil, "restarted", 1, nil},
		{"an execution in flight", "v0.105.0", 2, nil, nil, true, nil, "busy", 0, []string{"2 workflow execution(s)", "never interrupts a run", "monoagentcli daemon restart"}},
		{"saved API settings that cannot be used", "v0.105.0", 0, damaged, nil, true, nil, "settings", 0, []string{"OpenAI-compatible API", "the saved settings are damaged"}},
		{"what cannot be told", "v0.105.0", 0, nil, errors.New("database is locked"), true, nil, "unknown", 0, []string{"database is locked"}},
		{"not registered for auto-start", "v0.105.0", 0, nil, nil, false, nil, "not_registered", 0, []string{"daemon install"}},
		{"a service manager that fails", "v0.105.0", 0, nil, nil, true, errors.New("launchctl: exit status 113"), "failed", 1, []string{"exit status 113", "daemon restart"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeAutostart{installed: c.registered, restartErr: c.restartErr}
			deps, hb := daemonDeps(t, "", c.daemon, c.inFlight, c.settings, c.count, fake, os.Getpid(), nil)
			fake.onRestart = beatsNewVersion(hb, "v0.108.0")
			d := deps.daemonAfterUpdate(context.Background(), "v0.108.0")
			if c.action == "" {
				if d != nil || fake.restarted != 0 {
					t.Fatalf("d = %+v, restarted %d: nothing to do", d, fake.restarted)
				}
				return
			}
			if d == nil || d.Action != c.action || fake.restarted != c.restarted || d.InFlight != c.inFlight {
				t.Fatalf("d = %+v, restarted %d: want %s (an update never interrupts a run)", d, fake.restarted, c.action)
			}
			for _, want := range c.says {
				if !strings.Contains(d.Message, want) {
					t.Errorf("message %q should say %q", d.Message, want)
				}
			}
		})
	}
}

// The service is per user: one that does not run this home's daemon is never restarted, and a
// restart is only reported once a live heartbeat of the new version follows.
func TestDaemonAfterUpdateChecksTheServiceAndVerifiesTheRestart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		pid       int
		pidErr    error
		beats     bool // the restarted daemon writes a heartbeat of the new version
		action    string
		restarted int
		says      string
	}{
		{"the service runs another pid", os.Getpid() + 1, nil, true, "not_managed", 0, "not this daemon"},
		{"the pid cannot be told", 0, autostart.ErrPIDUnverifiable, true, "not_managed", 0, "manually"},
		{"the service runs this daemon", os.Getpid(), nil, true, "restarted", 1, "Restarted the daemon"},
		{"no heartbeat follows", os.Getpid(), nil, false, "failed", 1, "monoagentcli daemon start"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeAutostart{installed: true}
			deps, hb := daemonDeps(t, "", "v0.105.0", 0, nil, nil, fake, c.pid, c.pidErr)
			if c.beats {
				fake.onRestart = beatsNewVersion(hb, "v0.108.0")
			}
			d := deps.daemonAfterUpdate(context.Background(), "v0.108.0")
			if d == nil || d.Action != c.action || fake.restarted != c.restarted || !strings.Contains(d.Message, c.says) {
				t.Fatalf("d = %+v, restarted %d; want %s / %d / %q", d, fake.restarted, c.action, c.restarted, c.says)
			}
		})
	}
}

// `update` on a binary that is already current still brings a stale daemon onto it: the first update
// to a release is made by the binary that predates it, which cannot restart anything.
func TestUpdateWhenAlreadyCurrentRestartsAStaleDaemon(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9","assets":[]}`)
	}))
	t.Cleanup(srv.Close)
	fake := &fakeAutostart{installed: true}
	d, hb := daemonDeps(t, srv.URL, "v9.8.0", 0, nil, nil, fake, os.Getpid(), nil)
	fake.onRestart = beatsNewVersion(hb, "v9.9.9")
	d.Version = func() string { return "v9.9.9" }
	d.JSONOutput = func() bool { return false }
	if err := d.run(NewCommand(d)); err != nil || fake.restarted != 1 {
		t.Fatalf("err %v, the stale daemon was restarted %d times, want 1", err, fake.restarted)
	}
}

// update --app --json reports what it did about the daemon, and the progress line carries it to the
// desktop app (the install path is linux/amd64's, as in the other update --app tests).
func TestUpdateAppRestartsTheDaemonAndReportsIt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("linux/amd64 install path")
	}
	latest := fakeAppRelease(t, tgz(t, map[string]string{"MonoAgent-linux-amd64": "new-app"}), false)
	app, _ := appInstallDir(t)
	fake := &fakeAutostart{installed: true}
	d, hb := daemonDeps(t, latest, "v1.0.0", 0, nil, nil, fake, os.Getpid(), nil)
	fake.onRestart = beatsNewVersion(hb, "v9.9.9")
	res, progress := runUpdateAppCmd(t, d, "--app", app, "--current", "v1.0.0")
	if !res.Success || res.Daemon == nil || res.Daemon.Action != "restarted" || fake.restarted != 1 || !strings.Contains(progress, "Restarted the daemon") {
		t.Fatalf("res = %+v, daemon %+v, restarted %d, progress %q", res, res.Daemon, fake.restarted, progress)
	}
}
