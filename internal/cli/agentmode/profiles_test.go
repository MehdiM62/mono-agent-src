package agentmode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBwrapProfileStructure(t *testing.T) {
	out, err := Profile(Bwrap)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range readOnlyHomePaths {
		if !strings.Contains(out, p) {
			t.Errorf("read-only path %q missing from the profile", p)
		}
	}
	if !strings.Contains(out, `--ro-bind "$HOME/$p" "$HOME/$p"`) {
		t.Error("persistence paths are not re-bound read-only")
	}
	for _, p := range append(append([]string{}, hiddenHomeDirs...), hiddenHomeFiles...) {
		if !strings.Contains(out, p) {
			t.Errorf("hidden path %q missing from the profile", p)
		}
	}
	for _, f := range []string{"--unshare-pid", "--unshare-ipc", "--new-session", "--cap-drop ALL", "--clearenv", "--proc /proc",
		`--tmpfs "$HOME/.monoagent"`, `--tmpfs "$HOME/.gnupg"`, `--ro-bind /dev/null "$run/bus"`, "--die-with-parent"} {
		if !strings.Contains(out, f) {
			t.Errorf("profile lacks %q", f)
		}
	}
	// Optional paths must be guarded, or bwrap fails (and --tmpfs would create the directory).
	if !strings.Contains(out, `[ -e "$HOME/$p" ]`) || !strings.Contains(out, `[ -d "$HOME/$p" ]`) {
		t.Error("optional paths are not guarded by an existence check")
	}
}

func TestBwrapEnvAllowlist(t *testing.T) {
	for _, bad := range []string{"SSH_AUTH_SOCK", "XDG_RUNTIME_DIR", "AWS_SECRET_ACCESS_KEY", "DBUS_SESSION_BUS_ADDRESS"} {
		for _, v := range envAllowlist {
			if v == bad {
				t.Errorf("%s must not be passed into the sandbox", bad)
			}
		}
	}
	out, _ := Profile(Bwrap)
	if !strings.Contains(out, "MONOAGENT_API_TOKEN") || !strings.Contains(out, "PATH") {
		t.Error("allowlist not in the profile")
	}
}

func TestSeatbeltProfileDeniesPersistence(t *testing.T) {
	out, _ := Profile(Seatbelt)
	for _, w := range []string{"Library/LaunchAgents", "/.bashrc", "/.zshrc", "/.profile", "/.ssh", "(deny file-write*"} {
		if !strings.Contains(out, w) {
			t.Errorf("seatbelt profile lacks %q:\n%s", w, out)
		}
	}
	if strings.Count(out, "(") != strings.Count(out, ")") {
		t.Errorf("unbalanced parentheses:\n%s", out)
	}
}

// Runs a command under the generated script with a scratch HOME. Skipped without bwrap
// or without user namespaces.
func TestBwrapProfileRealRun(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	if out, err := exec.Command("bwrap", "--bind", "/", "/", "true").CombinedOutput(); err != nil {
		t.Skipf("bwrap cannot create namespaces here: %s", out)
	}
	home := t.TempDir()
	for _, d := range []string{".monoagent", ".config/autostart", ".ssh"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{".monoagent/monoagent.db": "secret", ".bashrc": "orig", ".ssh/id_x": "key"}
	for f, c := range files {
		if err := os.WriteFile(filepath.Join(home, f), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script, _ := Profile(Bwrap)
	path := filepath.Join(t.TempDir(), "sandbox.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	probe := `
echo "db=$(cat "$HOME/.monoagent/monoagent.db" 2>/dev/null || echo none)"
echo "ssh=$(cat "$HOME/.ssh/id_x" 2>/dev/null || echo none)"
( echo x >> "$HOME/.bashrc" ) 2>/dev/null && echo "bashrc=writable" || echo "bashrc=readonly"
( echo x > "$HOME/.config/autostart/x.desktop" ) 2>/dev/null && echo "autostart=writable" || echo "autostart=readonly"
echo "procs=$(ls /proc | grep -c '^[0-9][0-9]*$')"
echo "sock=${SSH_AUTH_SOCK:-unset}"
echo "api=${MONOAGENT_API_URL:-unset}"
echo "aws=${AWS_SECRET_ACCESS_KEY:-unset}"
`
	cmd := exec.Command("sh", path, "sh", "-c", probe)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "SSH_AUTH_SOCK=/tmp/agent.sock",
		"AWS_SECRET_ACCESS_KEY=zzz", "MONOAGENT_API_URL=http://127.0.0.1:1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox run: %v\n%s", err, out)
	}
	got := string(out)
	for _, w := range []string{"db=none", "ssh=none", "bashrc=readonly", "autostart=readonly", "sock=unset", "aws=unset", "api=http://127.0.0.1:1"} {
		if !strings.Contains(got, w) {
			t.Errorf("want %q in:\n%s", w, got)
		}
	}
	if strings.Contains(got, "secret") {
		t.Errorf("state leaked:\n%s", got)
	}
	if !strings.Contains(got, "procs=") || strings.Contains(got, "procs=0") {
		t.Fatalf("no proc count:\n%s", got)
	}
	// Only the sandbox's own processes: bwrap-init/sh/ls/grep, far below a host's count.
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "procs=") {
			var c int
			if _, err := fmt.Sscan(strings.TrimPrefix(l, "procs="), &c); err == nil && c > 10 {
				t.Errorf("host processes visible (%d):\n%s", c, got)
			}
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".bashrc")); string(b) != "orig" {
		t.Errorf(".bashrc changed on the host: %q", b)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/autostart/x.desktop")); err == nil {
		t.Error("autostart entry was created on the host")
	}
}
