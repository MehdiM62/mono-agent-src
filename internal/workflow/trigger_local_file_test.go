package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestWatcher(t *testing.T, cfg map[string]interface{}) (*fileWatcher, *[]string) {
	t.Helper()
	c, err := parseLocalFileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	w := &fileWatcher{cfg: c, fire: func(items []Item) {
		got = append(got, items[0].JSON["event"].(string)+" "+filepath.Base(items[0].JSON["path"].(string)))
	}}
	if w.prev, err = w.scan(); err != nil {
		t.Fatal(err)
	}
	return w, &got
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalFileTrigger_Events(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "old.txt"), "x")
	w, got := newTestWatcher(t, map[string]interface{}{"path": dir, "ignore": "*.tmp"})

	writeFile(t, filepath.Join(dir, "new.txt"), "a")
	writeFile(t, filepath.Join(dir, "skip.tmp"), "a")
	writeFile(t, filepath.Join(dir, "sub", "deep.txt"), "a") // not recursive
	writeFile(t, filepath.Join(dir, "old.txt"), "longer")
	w.poll()
	if s := strings.Join(*got, ","); s != "add new.txt,change old.txt" {
		t.Fatalf("events = %q", s)
	}

	*got = nil
	if err := os.Remove(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	w.poll()
	if s := strings.Join(*got, ","); s != "unlink new.txt" {
		t.Fatalf("events = %q", s)
	}
}

func TestLocalFileTrigger_RecursiveAndEventFilter(t *testing.T) {
	dir := t.TempDir()
	w, got := newTestWatcher(t, map[string]interface{}{
		"path": dir, "recursive": true, "events": []interface{}{"add"}, "ignore": ".git",
	})
	writeFile(t, filepath.Join(dir, "sub", "deep.txt"), "a")
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "a")
	w.poll()
	writeFile(t, filepath.Join(dir, "sub", "deep.txt"), "changed")
	w.poll()
	if s := strings.Join(*got, ","); s != "add deep.txt" {
		t.Fatalf("events = %q", s)
	}
}

func TestLocalFileTrigger_SingleFileNotYetThere(t *testing.T) {
	p := filepath.Join(t.TempDir(), "later.csv")
	w, got := newTestWatcher(t, map[string]interface{}{"path": p})
	writeFile(t, p, "a")
	w.poll()
	if s := strings.Join(*got, ","); s != "add later.csv" {
		t.Fatalf("events = %q", s)
	}
}

func TestLocalFileTrigger_Config(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"path": "relative/dir"},
		{"path": "/tmp", "events": "add,rename"},
		{"path": "/tmp", "ignore": "[bad"},
	} {
		if _, err := parseLocalFileConfig(cfg); err == nil {
			t.Errorf("%v: want an error", cfg)
		}
	}
}

func TestLocalFileTrigger_ActivateFiresAndStops(t *testing.T) {
	dir := t.TempDir()
	fired := make(chan Item, 4)
	node := &WorkflowNode{ID: "n", Type: LocalFileTriggerType, Config: map[string]interface{}{"path": dir, "interval_seconds": 1.0}}
	stop, err := localFileSource{}.Activate(&Workflow{ID: "wf"}, node, func(items []Item) { fired <- items[0] })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	select {
	case it := <-fired:
		if it.JSON["event"] != "add" || it.JSON["path"] != filepath.Join(dir, "a.txt") {
			t.Fatalf("item = %v", it.JSON)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	stop()
	stop() // idempotent
}
