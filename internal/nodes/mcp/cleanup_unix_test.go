//go:build !windows

package mcpnode

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// A cancelled run leaves neither the server nor anything it started.
func TestStdioProcessCleanupOnCancel(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	cfg := stdioConfig(map[string]interface{}{"tool": "{{ $json.tool }}"})
	cfg["env"] = map[string]interface{}{fakeServerEnv: "1", "FAKE_PIDFILE": pidfile}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel once the first item's call has recorded the pids.
		for i := 0; i < 200; i++ {
			if _, err := os.Stat(pidfile); err == nil {
				time.Sleep(100 * time.Millisecond)
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	_, err := run(t, ctx, []map[string]interface{}{{"tool": "spawn"}, {"tool": "sleep"}}, cfg)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("cancel took %s", d)
	}
	b, rerr := os.ReadFile(pidfile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, f := range strings.Fields(string(b)) {
		pid, _ := strconv.Atoi(f)
		deadline := time.Now().Add(3 * time.Second)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("process %d outlived the node", pid)
		}
	}
}

// A normal finish also ends the server.
func TestStdioProcessCleanupOnFinish(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	cfg := stdioConfig(map[string]interface{}{"tool": "spawn"})
	cfg["env"] = map[string]interface{}{fakeServerEnv: "1", "FAKE_PIDFILE": pidfile}
	if _, err := run(t, context.Background(), nil, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(pidfile)
	for _, f := range strings.Fields(string(b)) {
		pid, _ := strconv.Atoi(f)
		deadline := time.Now().Add(3 * time.Second)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("process %d outlived the node", pid)
		}
	}
}
