//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const queueListJSON = `{"items":[{"id":"20261015-120000-abcdef","kind":"copy-style","status":"queued","cwd":"/w/app"}]}`

func TestApp_CoderQueue_ShellsOutToTheCLI(t *testing.T) {
	bin, argsLog := chatFakeCLI(t,
		fakeChatReply{match: "coder queue list", stdout: queueListJSON + "\n"},
		fakeChatReply{match: "coder queue start", stdout: `{"conversationId":"c1","turnId":"7a1c3f9e-2b4d-4e8f-9a0b-1c2d3e4f5a6b","prompt":"p","attachments":["/w/app/.monoagent/context/x/page.png"]}` + "\n"},
		fakeChatReply{match: "coder queue dismiss", stdout: `{"id":"20261015-120000-abcdef","dismissed":true}` + "\n"},
		fakeChatReply{match: "coder queue image", stdout: `{"id":"20261015-120000-abcdef","name":"page.png","dataUrl":"data:image/png;base64,AA=="}` + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	a.selectForTest("work")
	id := "20261015-120000-abcdef"

	if got := a.QueueList(); got != queueListJSON {
		t.Errorf("QueueList = %s", got)
	}
	if got := a.QueueStart(id, "/w/other"); !strings.Contains(got, `"conversationId":"c1"`) {
		t.Errorf("QueueStart = %s", got)
	}
	if got := a.QueueStart(id, ""); !strings.Contains(got, `"turnId"`) {
		t.Errorf("QueueStart without a folder = %s", got)
	}
	if got := a.QueueDismiss(id); !strings.Contains(got, `"dismissed":true`) {
		t.Errorf("QueueDismiss = %s", got)
	}
	if got := a.QueueImage(id); !strings.Contains(got, `"dataUrl":"data:image/png;base64,AA=="`) {
		t.Errorf("QueueImage = %s", got)
	}
	// An id that is not one never reaches the CLI (it could read as a flag).
	for _, got := range []string{a.QueueStart("--db-path=/x", ""), a.QueueDismiss("../x"), a.QueueImage("")} {
		if !strings.Contains(got, `"error"`) {
			t.Errorf("bad id answered %s", got)
		}
	}

	var calls []string
	for _, line := range readArgsLog(t, argsLog) {
		calls = append(calls, strings.TrimPrefix(line, "--profile work --json "))
	}
	want := []string{
		"coder queue list",
		"coder queue start --cwd=/w/other -- " + id,
		"coder queue start -- " + id,
		"coder queue dismiss -- " + id,
		"coder queue image -- " + id + " page.png",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestCoderQueueWatcherNoticesItems(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "coder-queue")
	var changes atomic.Int32
	w := newCoderQueueWatcher(dir, 10*time.Millisecond, func() { changes.Add(1) })
	w.Start()
	defer w.Stop()

	waitChange := func(n int32, what string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for changes.Load() < n {
			if time.Now().After(deadline) {
				t.Fatalf("no change event after %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	item := filepath.Join(dir, "20261015-120000-abcdef")
	// A folder the CLI is still filling (no item.json, or a .tmp- one) is
	// not an item yet.
	if err := os.MkdirAll(filepath.Join(dir, ".tmp-20261015-120000-abcdef"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(item, 0o700); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if changes.Load() != 0 {
		t.Fatalf("unfinished folders counted: %d", changes.Load())
	}
	if err := os.WriteFile(filepath.Join(item, "item.json"), []byte(`{"status":"queued"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChange(1, "an item landed")
	if err := os.WriteFile(filepath.Join(item, "item.json"), []byte(`{"status":"started","conversationId":"c1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChange(2, "an item was started")
	if err := os.RemoveAll(item); err != nil {
		t.Fatal(err)
	}
	waitChange(3, "an item was dismissed")
}
