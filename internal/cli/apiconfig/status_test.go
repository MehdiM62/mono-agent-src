package apiconfig

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apiconfig"
)

// `api status` reads the process environment and the daemon's heartbeat (apiconfig.Env{}), so
// its command test sets them and runs serially; main's api_status_test.go has the end-to-end cases.

func TestAPIStatusCommandWithoutADaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/models":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MONOAGENT_DAEMON_HEARTBEAT", filepath.Join(t.TempDir(), "none.json"))
	t.Setenv("MONOAGENT_API_CONFINEMENT", "")
	t.Setenv("MONOAGENT_HTTPAPI_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("MONOAGENT_API_V1_ADDR", "127.0.0.1:1")

	f := configTest(t)
	run := func(jsonOut bool) string {
		d := f.deps(jsonOut)
		d.ProfileID = func() string { return "default" }
		cmd := d.NewStatusCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	var st apiconfig.StatusReport
	if err := json.Unmarshal([]byte(run(true)), &st); err != nil {
		t.Fatal(err)
	}
	if st.Profile != "default" || st.Daemon.Running || len(st.Listeners) != 2 || !st.Listeners[0].V1Answers || st.Listeners[1].Reachable {
		t.Fatalf("status: %+v", st)
	}
	text := run(false)
	for _, want := range []string{"Profile default: 0 active API key(s)\n", "Daemon: not running\n", "  main ", "serves /v1 over http", "  v1   127.0.0.1:1  not reachable"} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lacks %q:\n%s", want, text)
		}
	}
}

func TestListenerNote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		l    apiconfig.ListenerReport
		want string
	}{
		{apiconfig.ListenerReport{}, "not reachable"},
		{apiconfig.ListenerReport{Reachable: true, DaemonSaysNoV1: true}, "the daemon does not serve /v1"},
		{apiconfig.ListenerReport{Reachable: true}, "bound off-loopback"},
		{apiconfig.ListenerReport{Reachable: true, V1: true}, "restart it"},
		{apiconfig.ListenerReport{Reachable: true, V1: true, V1Answers: true, Scheme: "https", ConfinementSource: "daemon"}, "serves /v1 over https"},
		{apiconfig.ListenerReport{Reachable: true, V1: true, V1Answers: true, ConfinementSource: "environment"}, "assumed from this shell's environment"},
	} {
		if got := listenerNote(tc.l); !strings.Contains(got, tc.want) {
			t.Errorf("listenerNote(%+v) = %q, want it to say %q", tc.l, got, tc.want)
		}
	}
	ls := []apiconfig.ListenerReport{{Name: "main"}}
	if !hasListener(ls, "main") || hasListener(ls, "v1") {
		t.Error("hasListener")
	}
}
