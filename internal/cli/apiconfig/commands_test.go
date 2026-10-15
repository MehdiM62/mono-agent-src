package apiconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/storage"
)

func TestAPIConfigShowJSON(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{installed: true})
	saveAt(t, db, "max_concurrent=8")

	out, stderr, err := f.run(t, true, "show")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("stderr: %q", stderr)
	}
	r := decodeConfig(t, out)
	if r.V != 1 || r.Environment != "shell" || len(r.Settings) != 10 || r.RestartNeeded || len(r.Problems) != 0 {
		t.Fatalf("report: %+v", r)
	}
	if r.Daemon.Running || r.Daemon.ReportsSettings || !r.Daemon.Autostart {
		t.Errorf("daemon: %+v", r.Daemon)
	}
	for _, row := range r.Settings {
		want := "not_running"
		if row.State != want {
			t.Errorf("%s: state %s, want %s", row.Key, row.State, want)
		}
		if row.Key == "max_concurrent" && (row.Saved != "8" || row.Effective != "8" || row.Source != "saved") {
			t.Errorf("max_concurrent: %+v", row)
		}
	}
	// One document, and nothing written by `show`.
	if strings.Count(out, "\n{") != 0 || savedAt(t, db).MaxConcurrent != "8" {
		t.Errorf("show changed something or printed more than a document")
	}
}

func TestAPIConfigShowText(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{installed: true})
	saveAt(t, db, "v1_addr=0.0.0.0:9443", "confinement=sandboxed")
	f.setenv("MONOAGENT_API_MAX_CONCURRENT", "12")

	out, _, err := f.run(t, false, "show")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SETTING", "SAVED", "EFFECTIVE", "RUNNING", "STATE",
		"v1_addr", "0.0.0.0:9443 (saved)", "confinement", "sandboxed", "max_concurrent", "12 (env)",
		"not running", "daemon restart",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the text must say %q:\n%s", want, out)
		}
	}
	// The shell's variable is over what is saved here, and a daemon started by a service
	// does not read it: the text says which setting it is.
	f.setenv("MONOAGENT_API_CONFINEMENT", "chat-only")
	out, _, err = f.run(t, false, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "shell's environment overrides the saved value of: confinement") {
		t.Errorf("the text must say that this shell's environment overrides the saved confinement:\n%s", out)
	}
}

func TestAPIConfigShowAgainstALiveDaemon(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{installed: true})
	if err := f.beat(daemonhb.Heartbeat{PID: os.Getpid(), APISettings: map[string]daemonhb.APISetting{
		"max_concurrent": {Value: "4", Source: "default"},
		"turn_timeout":   {Value: "5m", Source: "env"},
	}}); err != nil {
		t.Fatal(err)
	}
	saveAt(t, db, "max_concurrent=8", "turn_timeout=20m")

	out, _, err := f.run(t, true, "show")
	if err != nil {
		t.Fatal(err)
	}
	r := decodeConfig(t, out)
	states := map[string]string{}
	for _, row := range r.Settings {
		states[row.Key] = row.State
	}
	if !r.Daemon.Running || !r.Daemon.ReportsSettings || !r.RestartNeeded || states["max_concurrent"] != "pending_restart" || states["turn_timeout"] != "overridden" || states["v1_addr"] != "unknown" {
		t.Errorf("daemon %+v, restart needed %v, states %v", r.Daemon, r.RestartNeeded, states)
	}
	text, _, err := f.run(t, false, "show")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"restart needed", "Restart needed for: max_concurrent", "monoagentcli daemon restart", "interrupts", "overridden", "turn_timeout", "max_concurrent",
		"4 (default)", "5m (env)"} { // what the daemon runs, and where each value came from

		if !strings.Contains(text, want) {
			t.Errorf("the text must say %q:\n%s", want, text)
		}
	}
}

func TestAPIConfigShowSaysWhenTheDaemonCannotBeRestarted(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{installed: false})
	if err := f.beat(daemonhb.Heartbeat{PID: os.Getpid(), APISettings: map[string]daemonhb.APISetting{"max_concurrent": {Value: "4", Source: "default"}}}); err != nil {
		t.Fatal(err)
	}
	saveAt(t, db, "max_concurrent=8")
	text, _, err := f.run(t, false, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "not registered for auto-start") || !strings.Contains(text, "daemon install") {
		t.Errorf("the text must say what to do when the daemon cannot be restarted by `daemon restart`:\n%s", text)
	}
}

func TestAPIConfigShowReportsProblemsAndStillWorks(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	saveAt(t, db, "confinement=everything-supersecret")
	out, _, err := f.run(t, true, "show")
	if err != nil {
		t.Fatalf("an invalid saved value must not stop show: %v", err)
	}
	if r := decodeConfig(t, out); len(r.Problems) != 1 || r.Problems[0].Key != "confinement" {
		t.Errorf("problems: %+v", r.Problems)
	}
	text, _, err := f.run(t, false, "show")
	if err != nil || !strings.Contains(text, "confinement must be") || !strings.Contains(text, "api config set --confinement") || !strings.Contains(text, "api config unset confinement") {
		t.Errorf("the text names the problem and how to remove the value: %v\n%s", err, text)
	}
}

func TestAPIConfigShowOfADocumentItCannotReadIsAnError(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	st, _ := storage.NewDatabase(db)
	if _, err := st.DB.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, apiconfig.Row, `{"v":2}`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	_, _, err := f.run(t, true, "show")
	if err == nil || code(err) != 1 || !strings.Contains(err.Error(), apiconfig.Row) {
		t.Errorf("exit %d, %v", code(err), err)
	}
}

func TestAPIConfigSetSavesAndReports(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{installed: true})
	out, stderr, err := f.run(t, true, "set", "--max-concurrent", "8", "--turn-timeout", "900s", "--image-runtimes", "agy, Codex")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("stderr: %q", stderr)
	}
	r := decodeChange(t, out)
	if !r.Applied || strings.Join(r.Changed, ",") != "max_concurrent,turn_timeout,image_runtimes" || len(r.Widening) != 0 || len(r.Settings) != 10 {
		t.Errorf("result: applied %v, changed %v, widening %+v", r.Applied, r.Changed, r.Widening)
	}
	saved := savedAt(t, db)
	if saved.MaxConcurrent != "8" || saved.TurnTimeout != "15m" || saved.ImageRuntimes != "antigravity,codex" {
		t.Errorf("saved: %+v", saved)
	}
}

func TestAPIConfigSetText(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	f.useInstaller(&fakeAutostart{installed: true})
	out, _, err := f.run(t, false, "set", "--max-concurrent", "8")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Saved: max_concurrent") || !strings.Contains(out, "No daemon is running") {
		t.Errorf("the text must say what was saved and that nothing needs restarting:\n%s", out)
	}
	// Nothing changed.
	out, _, err = f.run(t, false, "set", "--max-concurrent", "8")
	if err != nil || !strings.Contains(out, "Nothing changed") {
		t.Errorf("the same value again: %v\n%s", err, out)
	}
}

func TestAPIConfigSetSaysThatARunningDaemonNeedsARestart(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	if err := f.beat(daemonhb.Heartbeat{PID: os.Getpid(), APISettings: map[string]daemonhb.APISetting{"max_concurrent": {Value: "4", Source: "default"}, "turn_timeout": {Value: "5m", Source: "flag"}}}); err != nil {
		t.Fatal(err)
	}
	for registered, want := range map[bool][]string{
		true:  {"restarts", "monoagentcli daemon restart", "interrupts"},
		false: {"not registered for auto-start", "daemon install"},
	} {
		f.useInstaller(&fakeAutostart{installed: registered})
		saveAt(t, db, "max_concurrent=4", "turn_timeout=30m") // as it is at the start of each round
		out, _, err := f.run(t, false, "set", "--max-concurrent", "8", "--turn-timeout", "20m")
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range append(want, "Overridden by the daemon's own flags or environment", "turn_timeout") { // turn_timeout was given to the daemon by a flag
			if !strings.Contains(out, w) {
				t.Errorf("registered %v: the text must say %q:\n%s", registered, w, out)
			}
		}
	}
}

func TestAPIConfigSetNeedsASetting(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	_, _, err := f.run(t, true, "set")
	if code(err) != 3 || err == nil || !strings.Contains(err.Error(), "nothing to change") || !strings.Contains(err.Error(), "such as --max-concurrent 8") {
		t.Errorf("exit %d, %v", code(err), err)
	}
	_, _, err = f.run(t, true, "set", "--yes")
	if code(err) != 3 || err == nil || !strings.Contains(err.Error(), "such as --max-concurrent 8") {
		t.Errorf("--yes alone is no setting: exit %d, %v", code(err), err)
	}
	_, _, err = f.run(t, true, "set", "--dry-run")
	if code(err) != 3 {
		t.Errorf("--dry-run alone is no setting: exit %d, %v", code(err), err)
	}
}

func TestAPIConfigSetRefusesInvalidValuesByName(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"a bad class":      {"--confinement", "everything-supersecret"},
		"a number":         {"--max-concurrent", "supersecret"},
		"too many":         {"--max-concurrent", "65"},
		"a padded timeout": {"--turn-timeout", " 15m"},
		"an address":       {"--v1-addr", "9443"},
		"a list":           {"--tool-runtimes", "co dex"},
		"an empty value":   {"--max-concurrent", ""},
		"a certificate":    {"--tls-cert-file", "/c.pem"},
	} {
		f := configTest(t)
		db := f.db
		_, _, err := f.run(t, true, append([]string{"set"}, args...)...)
		if code(err) != 3 || err == nil {
			t.Errorf("%s: exit %d, %v", name, code(err), err)
			continue
		}
		if strings.Contains(err.Error(), "supersecret") {
			t.Errorf("%s: the message repeats a value: %v", name, err)
		}
		if !savedAt(t, db).IsEmpty() {
			t.Errorf("%s: a refused change wrote", name)
		}
	}
	// The messages name the setting.
	_, _, err := configTest(t).run(t, true, "set", "--confinement", "x")
	if err == nil || !strings.Contains(err.Error(), "confinement must be chat-only, sandboxed or any") {
		t.Errorf("%v", err)
	}
}

func TestAPIConfigSetTheWideningGate(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{})

	// Refused: exit 3, the reasons and the way out on stderr, nothing on stdout, nothing saved.
	for _, jsonOut := range []bool{true, false} {
		out, stderr, err := f.run(t, jsonOut, "set", "--v1-addr", "0.0.0.0:9443")
		if code(err) != 3 || err == nil {
			t.Fatalf("json=%v: exit %d, %v", jsonOut, code(err), err)
		}
		if out != "" || stderr != "" {
			t.Errorf("json=%v: stdout %q, stderr %q: the error is the command's error, printed once by main", jsonOut, out, stderr)
		}
		for _, want := range []string{"reach further", "0.0.0.0:9443", "--yes"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("json=%v: the message %q must say %q", jsonOut, err.Error(), want)
			}
		}
		if !savedAt(t, db).IsEmpty() {
			t.Fatal("a refused change wrote")
		}
	}

	// A dry run says what would happen, whatever --yes says.
	out, _, err := f.run(t, true, "set", "--v1-addr", "0.0.0.0:9443", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	r := decodeChange(t, out)
	if r.Applied || len(r.Widening) != 1 || r.Widening[0].Key != "v1_addr" || strings.Join(r.Changed, ",") != "v1_addr" {
		t.Errorf("dry run: applied %v, widening %+v, changed %v", r.Applied, r.Widening, r.Changed)
	}
	if !savedAt(t, db).IsEmpty() {
		t.Fatal("a dry run wrote")
	}
	text, _, err := f.run(t, false, "set", "--v1-addr", "0.0.0.0:9443", "--dry-run")
	if err != nil || !strings.Contains(text, "Dry run") || !strings.Contains(text, "nothing was saved") || !strings.Contains(text, "--yes") {
		t.Errorf("the dry run text: %v\n%s", err, text)
	}

	// Confirmed: saved, and the reasons are shown.
	text, _, err = f.run(t, false, "set", "--v1-addr", "0.0.0.0:9443", "--yes")
	if err != nil || !strings.Contains(text, "Saved: v1_addr") || !strings.Contains(text, "reach further") || !strings.Contains(text, "0.0.0.0:9443") {
		t.Errorf("the confirmed text: %v\n%s", err, text)
	}
	if savedAt(t, db).V1Addr != "0.0.0.0:9443" {
		t.Error("the confirmed change was not saved")
	}
	out, _, err = f.run(t, true, "set", "--v1-addr", "10.0.0.5:9443", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if r := decodeChange(t, out); !r.Applied || len(r.Widening) != 0 {
		t.Errorf("every interface to one host narrows the bind, which needs no confirmation (P9): %+v", r.Widening)
	}
	// One host to another reaches somewhere new: it needs --yes too (P9), and says where from.
	_, _, err = f.run(t, true, "set", "--v1-addr", "192.168.1.10:9443")
	if code(err) != 3 || err == nil || !strings.Contains(err.Error(), "move from 10.0.0.5:9443 to 192.168.1.10:9443") || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("exit %d, %v: want 3, with the move and the way out", code(err), err)
	}
	if savedAt(t, db).V1Addr != "10.0.0.5:9443" {
		t.Error("a refused move wrote")
	}
}

func TestAPIConfigUnset(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{})
	saveAt(t, db, "max_concurrent=8", "turn_timeout=20m", "context_confinement=sandboxed")

	out, _, err := f.run(t, true, "unset", "turn_timeout", "max-concurrent", "v1-addr")
	if err != nil {
		t.Fatal(err)
	}
	r := decodeChange(t, out)
	if !r.Applied || strings.Join(r.Changed, ",") != "max_concurrent,turn_timeout" {
		t.Errorf("changed %v", r.Changed)
	}
	if s := savedAt(t, db); s.MaxConcurrent != "" || s.TurnTimeout != "" || s.ContextConfinement != "sandboxed" {
		t.Errorf("saved: %+v", s)
	}
	text, _, err := f.run(t, false, "unset", "context_confinement", "--yes") // narrowing: --yes is not needed, and is harmless
	if err != nil || !strings.Contains(text, "Removed: context_confinement") {
		t.Errorf("%v\n%s", err, text)
	}
	text, _, err = f.run(t, false, "unset", "max_concurrent")
	if err != nil || !strings.Contains(text, "Nothing changed") {
		t.Errorf("an unset of what is not saved: %v\n%s", err, text)
	}
}

func TestAPIConfigUnsetAll(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	db := f.db
	f.useInstaller(&fakeAutostart{})
	saveAt(t, db, "max_concurrent=8", "confinement=sandboxed", "image_runtimes=none")
	// Removing image_runtimes none switches image generation back on: that is a widening.
	_, _, err := f.run(t, true, "unset", "--all")
	if code(err) != 3 || err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("exit %d, %v", code(err), err)
	}
	out, _, err := f.run(t, true, "unset", "--all", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if r := decodeChange(t, out); strings.Join(r.Changed, ",") != "confinement,max_concurrent,image_runtimes" || len(r.Widening) == 0 {
		t.Errorf("changed %v, widening %+v", r.Changed, r.Widening)
	}
	if !savedAt(t, db).IsEmpty() {
		t.Error("something is still saved")
	}
}

func TestAPIConfigUnsetNeedsKeysOrAll(t *testing.T) {
	t.Parallel()
	f := configTest(t)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"nothing":        {[]string{}, "give the settings to unset, or --all"},
		"all and a key":  {[]string{"--all", "max_concurrent"}, "not both"},
		"an unknown key": {[]string{"nonsense-supersecret"}, "not a setting"},
	} {
		_, _, err := f.run(t, true, append([]string{"unset"}, c.args...)...)
		if code(err) != 3 || err == nil || strings.Contains(err.Error(), "supersecret") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: exit %d, %v, want a message with %q", name, code(err), err, c.want)
		}
	}
}
