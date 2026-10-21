//go:build devaccount && !windows

package accountsmoke

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A freshly installed extension in a real headless Chromium, against the real
// binary's bridge on a private port in a scratch HOME (the rig): no second
// sign-in and no `extension pair`. chrome-extension/autopair_e2e.mjs drives
// the browser. Skipped without node and a Chromium.
func TestExtensionPairsItselfInARealBrowser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	script, _ := filepath.Abs("../../chrome-extension/autopair_e2e.mjs")

	drive := func(t *testing.T, r *rig, mode string, onSignedOut func()) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, node, script, mode, strconv.Itoa(r.port("bridge")))
		// The browser's scratch files go under this process's TMPDIR in a short
		// folder (Chromium refuses a long profile path), never under /tmp unasked.
		tmp, err := os.MkdirTemp("", "ap")
		must(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(tmp) })
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp, "HOME="+r.home)
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		var stderr strings.Builder
		cmd.Stderr = &stderr
		must(t, cmd.Start())
		var out strings.Builder
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			out.WriteString(line + "\n")
			if line == "SIGNED_OUT_OK" && onSignedOut != nil {
				onSignedOut()
				_, _ = io.WriteString(stdin, "go\n")
			}
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("autopair_e2e.mjs %s: %v\nstdout: %s\nstderr: %s", mode, err, out.String(), stderr.String())
		}
		if strings.HasPrefix(out.String(), "SKIP") {
			t.Skip(strings.TrimSpace(out.String()))
		}
		if !strings.Contains(out.String(), "OK\n") {
			t.Fatalf("autopair_e2e.mjs %s did not finish: %s %s", mode, out.String(), stderr.String())
		}
		return out.String()
	}

	paired := func(t *testing.T, r *rig) []map[string]any {
		t.Helper()
		res := r.run("extension", "browsers", "list", "--json")
		mustExit(t, res, 0)
		var doc struct {
			Paired []map[string]any `json:"paired"`
		}
		must(t, json.Unmarshal([]byte(res.stdout), &doc))
		return doc.Paired
	}

	t.Run("signed in: the normal panel with no sign-in step", func(t *testing.T) {
		r := newRig(t, rigOptions{enforce: past})
		r.signIn()
		r.start("extension", "serve")
		r.waitFor("the bridge", 60*time.Second, func() bool { return bridgeUp(r) })
		drive(t, r, "signed-in", nil)
		list := paired(t, r)
		if len(list) != 1 {
			t.Fatalf("paired browsers = %v, want the one browser", list)
		}
		tok, _ := os.ReadFile(filepath.Join(r.home, ".monoagent", "extension.token"))
		file, _ := os.ReadFile(filepath.Join(r.home, ".monoagent", "extension.browsers.json"))
		if len(tok) == 0 || strings.Contains(string(file), strings.TrimSpace(string(tok))) {
			t.Fatal("the browsers file holds the shared token")
		}
		// Revoke: the browser's socket is closed and it cannot come back with that token.
		id, _ := list[0]["id"].(string)
		mustExit(t, r.run("extension", "browsers", "revoke", id), 0)
		if left := paired(t, r); len(left) != 0 {
			t.Fatalf("after revoke: %v", left)
		}
	})

	t.Run("signed out: the sign-in screen, then the panel flips by itself", func(t *testing.T) {
		r := newRig(t, rigOptions{enforce: past})
		r.start("extension", "serve")
		r.waitFor("the bridge", 60*time.Second, func() bool { return bridgeUp(r) })
		drive(t, r, "signed-out", func() {
			// The CLI is gated while signed out, so read the file the bridge writes.
			if b, err := os.ReadFile(filepath.Join(r.home, ".monoagent", "extension.browsers.json")); err == nil && strings.TrimSpace(string(b)) != "null" && strings.TrimSpace(string(b)) != "[]" {
				t.Errorf("a browser paired while signed out: %s", b)
			}
			r.signIn()
		})
		if got := paired(t, r); len(got) != 1 {
			t.Fatalf("paired browsers after signing in = %v, want 1", got)
		}
	})
}

func bridgeUp(r *rig) bool {
	res, err := http.Get("http://127.0.0.1:" + strconv.Itoa(r.port("bridge")) + "/monoagent/health")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == 200
}
