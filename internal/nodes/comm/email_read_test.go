package comm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeUIDIMAP is a minimal IMAP server for readIMAPMail: LOGIN, EXAMINE
// with UIDVALIDITY, UID SEARCH (ALL / UNSEEN / UID n:*) and UID FETCH of a
// comma-separated UID set.
type fakeUIDIMAP struct {
	mu          sync.Mutex
	uidValidity uint32
	msgs        map[uint32]bool // uid -> \Seen
	commands    []string
	failFetch   bool
}

func (f *fakeUIDIMAP) add(uid uint32, seen bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs[uid] = seen
}

func (f *fakeUIDIMAP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(format string, args ...interface{}) { fmt.Fprintf(conn, format+"\r\n", args...) }
	w("* OK fake IMAP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.mu.Lock()
		f.commands = append(f.commands, line)
		tag, rest, _ := strings.Cut(line, " ")
		fields := strings.Fields(rest)
		switch {
		case fields[0] == "LOGIN":
			w("%s OK LOGIN completed", tag)
		case fields[0] == "EXAMINE":
			w("* %d EXISTS", len(f.msgs))
			w("* OK [UIDVALIDITY %d] UIDs valid", f.uidValidity)
			w("%s OK [READ-ONLY] done", tag)
		case len(fields) > 1 && fields[0] == "UID" && fields[1] == "SEARCH":
			var low uint32 = 1
			unseen := false
			for i := 2; i < len(fields); i++ {
				switch fields[i] {
				case "UNSEEN":
					unseen = true
				case "UID":
					from, _, _ := strings.Cut(fields[i+1], ":")
					n, _ := strconv.Atoi(from)
					low = uint32(n)
					i++
				}
			}
			var hits []string
			var max uint32
			for uid, seen := range f.msgs {
				if uid > max {
					max = uid
				}
				if uid >= low && !(unseen && seen) {
					hits = append(hits, strconv.Itoa(int(uid)))
				}
			}
			// RFC 3501: "n:*" includes the highest UID even when it is below n.
			if low > max && max > 0 && !(unseen && f.msgs[max]) {
				hits = append(hits, strconv.Itoa(int(max)))
			}
			w("* SEARCH %s", strings.Join(hits, " "))
			w("%s OK SEARCH completed", tag)
		case len(fields) > 2 && fields[0] == "UID" && fields[1] == "FETCH":
			if f.failFetch {
				w("%s NO fetch failed", tag)
				break
			}
			var uids []int
			for _, s := range strings.Split(fields[2], ",") {
				n, _ := strconv.Atoi(s)
				uids = append(uids, n)
			}
			sort.Ints(uids)
			for i, uid := range uids {
				flags := ""
				if f.msgs[uint32(uid)] {
					flags = `\Seen`
				}
				body := fmt.Sprintf("body %d", uid)
				env := fmt.Sprintf(`("Mon, 1 Sep 2026 10:00:00 +0000" "Subject %d" (("Ann" NIL "ann" "example.com")) NIL NIL NIL NIL NIL NIL "<m%d@example.com>")`, uid, uid)
				fmt.Fprintf(conn, "* %d FETCH (UID %d FLAGS (%s) ENVELOPE %s BODY[TEXT]<0> {%d}\r\n%s)\r\n", i+1, uid, flags, env, len(body), body)
			}
			w("%s OK FETCH completed", tag)
		case fields[0] == "LOGOUT":
			w("* BYE")
			w("%s OK LOGOUT completed", tag)
			f.mu.Unlock()
			return
		default:
			w("%s BAD unknown command", tag)
		}
		f.mu.Unlock()
	}
}

func newFakeUIDIMAP(t *testing.T) *fakeUIDIMAP {
	f := &fakeUIDIMAP{uidValidity: 7, msgs: map[uint32]bool{}}
	prev := dialEmailIMAP
	t.Cleanup(func() { dialEmailIMAP = prev })
	dialEmailIMAP = func(ctx context.Context, host string, port int, useTLS bool) (net.Conn, error) {
		if host != "imap.test" || port != 993 || !useTLS {
			return nil, fmt.Errorf("dial %s:%d tls=%v", host, port, useTLS)
		}
		client, server := net.Pipe()
		go f.serve(server)
		return client, nil
	}
	return f
}

func newStateContext(t *testing.T) context.Context {
	t.Helper()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return vault.ContextWithDB(context.Background(), db.DB)
}

var emailInput = workflow.NodeInput{WorkflowID: "wf1", NodeID: "mail", ExecutionID: "e"}

func emailConfig(extra map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{"imap_host": "imap.test", "username": "me@example.com", "password": "pw"}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func readUIDs(t *testing.T, ctx context.Context, cfg map[string]interface{}) (string, error) {
	t.Helper()
	out, err := (&EmailReadNode{}).Execute(ctx, emailInput, cfg)
	if err != nil {
		return "", err
	}
	var uids []string
	for _, it := range out[0].Items {
		uids = append(uids, strconv.Itoa(it.JSON["uid"].(int)))
	}
	return strings.Join(uids, ","), nil
}

func TestEmailReadReadsNewestFirstWithoutMarkingSeen(t *testing.T) {
	f := newFakeUIDIMAP(t)
	for uid := uint32(1); uid <= 5; uid++ {
		f.add(uid, uid == 4)
	}
	out, err := (&EmailReadNode{}).Execute(context.Background(), emailInput, emailConfig(map[string]interface{}{"limit": 3.0}))
	if err != nil {
		t.Fatal(err)
	}
	items := out[0].Items
	if len(items) != 3 || items[0].JSON["uid"] != 5 || items[2].JSON["uid"] != 3 {
		t.Fatalf("items = %v, want the newest 3, newest first", items)
	}
	if items[1].JSON["read"] != true || items[0].JSON["read"] != false {
		t.Fatalf("read flags = %v", items)
	}
	if items[0].JSON["subject"] != "Subject 5" || items[0].JSON["body"] != "body 5" || items[0].JSON["message_id"] != "<m5@example.com>" {
		t.Fatalf("item = %v", items[0].JSON)
	}
	f.mu.Lock()
	commands := append([]string(nil), f.commands...)
	f.mu.Unlock()
	for _, c := range commands {
		up := strings.ToUpper(c)
		if strings.Contains(up, " SELECT ") || strings.Contains(up, " STORE ") {
			t.Errorf("command %q can change mailbox state", c)
		}
		if strings.Contains(up, " FETCH ") && !strings.Contains(up, "BODY.PEEK[") {
			t.Errorf("FETCH without BODY.PEEK: %q", c)
		}
	}

	if got, _ := readUIDs(t, context.Background(), emailConfig(map[string]interface{}{"unread_only": true})); got != "5,3,2,1" {
		t.Fatalf("unread_only = %q", got)
	}
}

func TestEmailReadSinceLastRun(t *testing.T) {
	f := newFakeUIDIMAP(t)
	ctx := newStateContext(t)
	cfg := emailConfig(map[string]interface{}{"limit": 2.0, "since_last_run": true})
	for uid := uint32(1); uid <= 4; uid++ {
		f.add(uid, false)
	}

	// First run: the newest limit messages, as without the flag.
	if got, err := readUIDs(t, ctx, cfg); err != nil || got != "4,3" {
		t.Fatalf("first run = %q, %v", got, err)
	}
	// Nothing new: "UID 5:*" still matches UID 4, which is dropped.
	if got, err := readUIDs(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("unchanged mailbox = %q, %v", got, err)
	}
	// A backlog over the limit is read oldest first across runs.
	for uid := uint32(5); uid <= 7; uid++ {
		f.add(uid, false)
	}
	if got, _ := readUIDs(t, ctx, cfg); got != "6,5" {
		t.Fatalf("backlog run 1 = %q, want 6,5", got)
	}
	if got, _ := readUIDs(t, ctx, cfg); got != "7" {
		t.Fatalf("backlog run 2 = %q, want 7", got)
	}

	// A failed fetch leaves the cursor where it was.
	f.add(8, false)
	f.mu.Lock()
	f.failFetch = true
	f.mu.Unlock()
	if _, err := readUIDs(t, ctx, cfg); err == nil {
		t.Fatal("failed fetch returned no error")
	}
	f.mu.Lock()
	f.failFetch = false
	f.mu.Unlock()
	if got, _ := readUIDs(t, ctx, cfg); got != "8" {
		t.Fatalf("run after the failure = %q, want 8", got)
	}

	// UIDVALIDITY changed: the mailbox was renumbered, start over.
	f.mu.Lock()
	f.uidValidity = 8
	f.msgs = map[uint32]bool{1: false, 2: false, 3: false}
	f.mu.Unlock()
	if got, _ := readUIDs(t, ctx, cfg); got != "3,2" {
		t.Fatalf("after UIDVALIDITY change = %q, want a first run", got)
	}
	f.add(4, false)
	if got, _ := readUIDs(t, ctx, cfg); got != "4" {
		t.Fatalf("after the reset = %q, want 4", got)
	}

	// Another mailbox has its own cursor.
	cfg["mailbox"] = "Archive"
	if got, _ := readUIDs(t, ctx, cfg); got != "4,3" {
		t.Fatalf("other mailbox = %q, want a first run", got)
	}
}

func TestEmailReadSinceLastRunUnreadOnly(t *testing.T) {
	f := newFakeUIDIMAP(t)
	ctx := newStateContext(t)
	cfg := emailConfig(map[string]interface{}{"since_last_run": true, "unread_only": true})
	f.add(1, false)
	f.add(2, true)
	if got, _ := readUIDs(t, ctx, cfg); got != "1" {
		t.Fatalf("first run = %q", got)
	}
	f.add(3, true)
	f.add(4, false)
	if got, _ := readUIDs(t, ctx, cfg); got != "4" {
		t.Fatalf("second run = %q, want 4", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(strings.Join(f.commands, "\n"), "UID SEARCH UID 2:* UNSEEN") {
		t.Fatalf("commands = %q, want a UID 2:* UNSEEN search", f.commands)
	}
}

func TestEmailReadSinceLastRunNeedsWorkflowState(t *testing.T) {
	newFakeUIDIMAP(t)
	_, err := (&EmailReadNode{}).Execute(context.Background(), emailInput, emailConfig(map[string]interface{}{"since_last_run": true}))
	if !errors.Is(err, workflow.ErrNoWorkflowState) {
		t.Fatalf("err = %v, want ErrNoWorkflowState", err)
	}
}

func TestEmailReadRequiresConnectionFields(t *testing.T) {
	for _, key := range []string{"imap_host", "username", "password"} {
		cfg := emailConfig(nil)
		delete(cfg, key)
		_, err := (&EmailReadNode{}).Execute(context.Background(), emailInput, cfg)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("without %s: err = %v", key, err)
		}
	}
}

// IMAP quoted strings escape only backslash and double quote; non-ASCII
// passwords go through unchanged rather than as Go \u escapes.
func TestIMAPQuote(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:    `"plain"`,
		`a"b\c`:    `"a\"b\\c"`,
		`pässwörd`: `"pässwörd"`,
	} {
		if got := imapQuote(in); got != want {
			t.Errorf("imapQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestEmailReadSinceLastRunBaseline(t *testing.T) {
	f := newFakeUIDIMAP(t)
	ctx := newStateContext(t)
	cfg := emailConfig(map[string]interface{}{"since_last_run": true, "baseline": true, "unread_only": true})
	f.add(1, false)
	f.add(2, true)

	// The first run returns nothing and fetches nothing; the cursor starts
	// at the highest UID, read or not.
	if got, err := readUIDs(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("baseline first run = %q, %v; want nothing", got, err)
	}
	f.mu.Lock()
	for _, c := range f.commands {
		if strings.Contains(c, "FETCH") {
			t.Errorf("baseline run fetched: %q", c)
		}
	}
	f.mu.Unlock()

	f.add(3, false)
	if got, _ := readUIDs(t, ctx, cfg); got != "3" {
		t.Fatalf("second run = %q, want 3", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(strings.Join(f.commands, "\n"), "UID SEARCH UID 3:* UNSEEN") {
		t.Fatalf("commands = %q, want a UID 3:* UNSEEN search", f.commands)
	}
}

func TestEmailReadSinceLastRunBaselineEmptyMailbox(t *testing.T) {
	f := newFakeUIDIMAP(t)
	ctx := newStateContext(t)
	cfg := emailConfig(map[string]interface{}{"since_last_run": true, "baseline": true})
	if got, err := readUIDs(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("empty mailbox = %q, %v", got, err)
	}
	f.add(1, false)
	if got, _ := readUIDs(t, ctx, cfg); got != "1" {
		t.Fatalf("first message after an empty baseline = %q, want 1", got)
	}
}
