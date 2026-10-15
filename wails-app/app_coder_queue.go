package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Queued coder chats (C5 of docs/mastermind/plans/2026-10-15-coder-browser.md):
// the browser extension queues a coder chat, the desktop shows it as a
// queued bubble, and Start here is what runs its first turn. Every binding
// shells out to `monoagentcli coder queue … --json` (logic lives in the
// CLI); the watcher only notices that the queue folder changed and tells
// the page (coder:queue), which lists again.

// coderQueueID is a queued item's id (the CLI checks it too); checked here
// so an id can never reach argv as a flag.
var coderQueueID = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

func badQueueID(id string) string {
	return aiError(fmt.Errorf("%q is not a queued coder chat id", id))
}

// QueueList returns `coder queue list`: {items}, the active profile's
// queued chats, newest first. The first call starts the folder watcher.
func (a *App) QueueList() string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	a.startCoderQueueWatch()
	return a.jsonResult("coder", "queue", "list")
}

// QueueStart creates the queued chat's coder conversation (in cwd, or ""
// for the folder it was queued for) and returns {conversationId, turnId,
// prompt, attachments, cwd, runtime, model}; the page then runs the turn
// with StartChatTurn and opens the bubble.
func (a *App) QueueStart(id, cwd string) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	if !coderQueueID.MatchString(id) {
		return badQueueID(id)
	}
	args := []string{"coder", "queue", "start"}
	if cwd != "" {
		args = append(args, "--cwd="+cwd)
	}
	return a.jsonResult(append(args, "--", id)...)
}

// QueueDismiss drops a queued chat and its page context.
func (a *App) QueueDismiss(id string) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	if !coderQueueID.MatchString(id) {
		return badQueueID(id)
	}
	return a.jsonResult("coder", "queue", "dismiss", "--", id)
}

// QueueImage returns the queued chat's page screenshot: {id, name, dataUrl}.
func (a *App) QueueImage(id string) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	if !coderQueueID.MatchString(id) {
		return badQueueID(id)
	}
	return a.jsonResult("coder", "queue", "image", "--", id, "page.png")
}

// coderQueueDir is where the CLI keeps queued chats: next to the database.
func (a *App) coderQueueDir() string {
	db := a.dbPath
	if db == "" {
		home, _ := os.UserHomeDir()
		db = filepath.Join(home, ".monoagent", "monoagent.db")
	}
	return filepath.Join(filepath.Dir(db), "coder-queue")
}

var coderQueueWatchOnce sync.Once

// startCoderQueueWatch starts the queue watcher once the app runs (a.ctx
// set): it lives as long as the app, like the queue folder itself.
func (a *App) startCoderQueueWatch() {
	if a.ctx == nil {
		return
	}
	coderQueueWatchOnce.Do(func() {
		w := newCoderQueueWatcher(a.coderQueueDir(), 0, func() {
			a.emitFolderEvent("coder:queue", map[string]interface{}{})
		})
		w.Start()
	})
}

// coderQueuePoll is how often the watcher looks at the queue folder. It
// lists one small folder: the cost of noticing an item a person just
// queued in the browser within two seconds.
const coderQueuePoll = 2 * time.Second

// coderQueueWatcher polls the queue folder and calls onChange when an
// item appears, goes, or its item.json is rewritten (started). It never
// reads an item; the page asks the CLI for the list. Polling, as the
// document and capture watchers do: the module has no fsnotify.
type coderQueueWatcher struct {
	dir      string
	interval time.Duration
	onChange func()
	last     string
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

func newCoderQueueWatcher(dir string, interval time.Duration, onChange func()) *coderQueueWatcher {
	if interval <= 0 {
		interval = coderQueuePoll
	}
	return &coderQueueWatcher{dir: dir, interval: interval, onChange: onChange, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}

// Start primes the snapshot silently (the page lists on mount anyway) and
// polls until Stop.
func (w *coderQueueWatcher) Start() {
	w.last = coderQueueSignature(w.dir)
	go func() {
		defer close(w.doneCh)
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-t.C:
				if sig := coderQueueSignature(w.dir); sig != w.last {
					w.last = sig
					w.onChange()
				}
			}
		}
	}()
}

func (w *coderQueueWatcher) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	<-w.doneCh
}

// coderQueueSignature names every finished item (one with an item.json)
// with its item.json's size and mtime. A missing folder is the empty queue.
func coderQueueSignature(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range entries {
		if !e.IsDir() || !coderQueueID.MatchString(e.Name()) {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, e.Name(), "item.json"))
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", e.Name(), fi.Size(), fi.ModTime().UnixNano()))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}
