package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/applications"
	"github.com/monoes/mono-agent/internal/applyfill"
	"github.com/monoes/mono-agent/internal/applypark"
	"github.com/monoes/mono-agent/internal/storage"
)

// browser is the fake browser and the fake `aitask submit` in one: each
// submitted fill is a task that is already complete, and left a tab open.
type browser struct {
	mu      sync.Mutex
	dbPath  string
	tabs    []applypark.LiveTab
	next    int
	started []string
	running int
	maxSeen int
	status  string // status of the tasks it makes
}

func (b *browser) install(t *testing.T, dbPath string) {
	t.Helper()
	b.dbPath, b.next, b.status = dbPath, 200, aitask.StatusComplete
	oldSubmit, oldOp, oldLive, oldPark, oldPoll := submitTask, FillStartWiring.RequireOperator, liveTabs, applyfill.Parking, pollEvery
	t.Cleanup(func() {
		submitTask, FillStartWiring.RequireOperator, liveTabs, applyfill.Parking, pollEvery = oldSubmit, oldOp, oldLive, oldPark, oldPoll
	})
	FillStartWiring.RequireOperator = func(string) error { return nil }
	pollEvery = time.Millisecond
	read := func(context.Context, string) ([]applypark.LiveTab, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		return append([]applypark.LiveTab(nil), b.tabs...), nil
	}
	applyfill.Parking = &applyfill.ParkingConfig{Live: read}
	liveTabs = func(ctx context.Context, d Deps) ([]applypark.LiveTab, error) { return read(ctx, d.ProfileID()) }
	submitTask = func(_ context.Context, args []string) ([]byte, error) {
		app := ""
		for i, a := range args {
			if a == "--application" {
				app = args[i+1]
			}
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		b.next++
		tab := b.next
		b.started = append(b.started, app)
		b.running++
		if b.running > b.maxSeen {
			b.maxSeen = b.running
		}
		b.tabs = append(b.tabs, applypark.LiveTab{TabID: tab, GroupTitle: "Coder · Fill", URL: "https://acme.example/apply"})
		db, err := storage.NewDatabase(b.dbPath)
		if err != nil {
			return nil, err
		}
		defer db.Close()
		result := fmt.Sprintf(`{"status":"filled","fields":[{"label":"Email","value":"a@b.example","source":"Facts"}],"tabId":%d,"origin":"https://acme.example"}`, tab)
		task, err := (&aitask.Store{DB: db.DB, Profile: "default"}).Insert(aitask.Task{ID: aitask.NewID(), Kind: applyfill.Kind,
			ConversationID: fmt.Sprintf("conv-%d", tab), Status: b.status, Goal: "g", ApplicationID: app, Access: aitask.AccessWorkspace, BudgetUSD: 1})
		if err != nil {
			return nil, err
		}
		if b.status == aitask.StatusComplete {
			if _, err := db.DB.Exec(`UPDATE ai_tasks SET result_json = ? WHERE id = ?`, result, task.ID); err != nil {
				return nil, err
			}
		}
		b.running-- // the fill is over as soon as it is submitted
		return []byte(fmt.Sprintf(`{"taskId":"%s","kind":"application-apply","conversationId":"conv-%d","status":"%s"}`, task.ID, tab, b.status)), nil
	}
}

func (b *browser) closeTab(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.tabs[:0]
	for _, t := range b.tabs {
		if t.TabID != id {
			out = append(out, t)
		}
	}
	b.tabs = out
}

type queueOut struct {
	Max      int `json:"max"`
	Outcomes []struct {
		ApplicationID string `json:"applicationId"`
		TaskID        string `json:"taskId"`
		Result        string `json:"result"`
		Error         string `json:"error"`
	} `json:"outcomes"`
}

func jobs(t *testing.T, dbPath string, n int) []string {
	t.Helper()
	var ids []string
	for i := 0; i < n; i++ {
		out, err := run(t, dbPath, true, "add", "--kind", "job", "--title", fmt.Sprintf("Engineer %d", i), "--company", "Acme", "--url", fmt.Sprintf("https://acme.example/%d", i))
		if err != nil {
			t.Fatal(err)
		}
		var r struct{ ID string }
		_ = json.Unmarshal([]byte(out), &r)
		ids = append(ids, r.ID)
	}
	return ids
}

func statusOf(t *testing.T, db *sql.DB, id string) applications.Status {
	t.Helper()
	a, err := applications.NewStore(db).Get(context.Background(), "default", id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Status
}

func TestFillQueueParksEachAndHoldsTheCap(t *testing.T) {
	dbPath := homeDB(t)
	ids := jobs(t, dbPath, 3)
	b := &browser{}
	b.install(t, dbPath)

	// The person submits the first application once two are parked: its slot frees for the third.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			time.Sleep(5 * time.Millisecond)
			b.mu.Lock()
			n := len(b.started)
			b.mu.Unlock()
			if n >= 2 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond) // the third must not have started yet
		b.mu.Lock()
		if len(b.started) != 2 {
			t.Errorf("%d fills started with two tabs parked and a cap of 2", len(b.started))
		}
		b.mu.Unlock()
		withDB(t, dbPath, func(db *sql.DB) {
			if err := applications.NewStore(db).SetStatus(context.Background(), "default", ids[0], applications.StatusApplied, applications.ActorUser, "sent"); err != nil {
				t.Error(err)
			}
		})
	}()
	out, err := run(t, dbPath, true, append([]string{"fill", "queue", "--max-parked", "2"}, ids...)...)
	<-done
	if err != nil {
		t.Fatalf("queue: %v (%s)", err, out)
	}
	var got queueOut
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Max != 2 || len(got.Outcomes) != 3 {
		t.Fatalf("output %q: %v", out, err)
	}
	for _, o := range got.Outcomes {
		if o.Result != applypark.ResultParked {
			t.Errorf("%s: %s %s", o.ApplicationID, o.Result, o.Error)
		}
	}
	withDB(t, dbPath, func(db *sql.DB) {
		for _, id := range ids[1:] {
			if s := statusOf(t, db, id); s != applications.StatusAwaitingSubmit {
				t.Errorf("%s: %s", id, s)
			}
			tab, _ := applypark.Store{DB: db, Profile: "default"}.Get(context.Background(), id)
			if tab == nil || tab.State != applypark.StateParked {
				t.Errorf("%s: tab %+v", id, tab)
			}
		}
		// The sent one is the person's, and the queue never made it.
		if s := statusOf(t, db, ids[0]); s != applications.StatusApplied {
			t.Errorf("first: %s", s)
		}
	})
}

func TestFillQueueMovesOnFromATaskThatNeedsYou(t *testing.T) {
	dbPath := homeDB(t)
	ids := jobs(t, dbPath, 2)
	b := &browser{}
	b.install(t, dbPath)
	b.status = aitask.StatusNeedsUser // the conversation ended with no result
	out, err := run(t, dbPath, true, append([]string{"fill", "queue", "--max-parked", "1"}, ids...)...)
	if err != nil {
		t.Fatalf("queue: %v (%s)", err, out)
	}
	var got queueOut
	_ = json.Unmarshal([]byte(out), &got)
	if len(got.Outcomes) != 2 || got.Outcomes[0].Result != applypark.ResultNeedsYou || got.Outcomes[1].Result != applypark.ResultNeedsYou {
		t.Fatalf("outcomes = %s", out)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		for _, id := range ids {
			if s := statusOf(t, db, id); s != applications.StatusNeedsYou {
				t.Errorf("%s: %s", id, s)
			}
			f, _ := applications.NewStore(db).GetFill(context.Background(), "default", id)
			if f.Status != applications.FillNeedsYou || f.Note == "" {
				t.Errorf("%s fill = %+v", id, f)
			}
		}
	})
}

func TestFillQueueRefusals(t *testing.T) {
	dbPath := homeDB(t)
	ids := jobs(t, dbPath, 1)
	b := &browser{}
	b.install(t, dbPath)
	for name, args := range map[string][]string{
		"cap too high":  {"fill", "queue", "--max-parked", "11", ids[0]},
		"cap too low":   {"fill", "queue", "--max-parked", "-1", ids[0]},
		"nothing named": {"fill", "queue"},
		"both":          {"fill", "queue", "--pending", ids[0]},
		"unknown id":    {"fill", "queue", "nope"},
	} {
		if out, err := run(t, dbPath, true, args...); err == nil {
			t.Errorf("%s: accepted (%s)", name, out)
		}
	}
	withDB(t, dbPath, func(db *sql.DB) {
		if err := applications.NewStore(db).SetStatus(context.Background(), "default", ids[0], applications.StatusAwaitingSubmit, applications.ActorSystem, "x"); err != nil {
			t.Fatal(err)
		}
	})
	if out, err := run(t, dbPath, true, "fill", "queue", ids[0]); err == nil || !strings.Contains(err.Error(), "only a pending or needs_you") {
		t.Fatalf("an awaiting_submit application was queued: %v (%s)", err, out)
	}
	if len(b.started) != 0 {
		t.Fatalf("a refused queue started %d fills", len(b.started))
	}
	// Operator-only, like fill start.
	FillStartWiring.RequireOperator = func(what string) error { return errors.New("only the user can " + what) }
	for _, sub := range [][]string{{"fill", "queue", ids[0]}, {"fill", "resume", ids[0]}, {"fill", "sweep"}} {
		if _, err := run(t, dbPath, true, sub...); err == nil || !strings.Contains(err.Error(), "only the user") {
			t.Errorf("%v by a non-operator: %v", sub, err)
		}
	}
	FillStartWiring.RequireOperator = nil
	if _, err := run(t, dbPath, true, "fill", "queue", ids[0]); err == nil {
		t.Error("an unwired operator check let queue through")
	}
}

func TestFillSweepAndTabAndResume(t *testing.T) {
	dbPath := homeDB(t)
	ids := jobs(t, dbPath, 2)
	b := &browser{}
	b.install(t, dbPath)
	if out, err := run(t, dbPath, true, append([]string{"fill", "queue", "--max-parked", "2"}, ids...)...); err != nil {
		t.Fatalf("queue: %v (%s)", err, out)
	}
	tabOf := func(id string) (tab *applypark.Tab, observed string) {
		out, err := run(t, dbPath, true, "fill", "tab", id)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Tab              *applypark.Tab `json:"tab"`
			SubmitObservedAt string         `json:"submitObservedAt"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r.Tab, r.SubmitObservedAt
	}
	first, _ := tabOf(ids[0])
	second, _ := tabOf(ids[1])
	if first == nil || second == nil || first.TabID == second.TabID {
		t.Fatalf("tabs = %+v %+v", first, second)
	}

	// A submit the extension saw is only a hint.
	withDB(t, dbPath, func(db *sql.DB) {
		if _, ok, err := applypark.Observe(context.Background(), db, applypark.Observation{TabID: first.TabID, Origin: first.Origin}); err != nil || !ok {
			t.Fatalf("observe: %v %v", ok, err)
		}
		if s := statusOf(t, db, ids[0]); s != applications.StatusAwaitingSubmit {
			t.Fatalf("an observed submit moved the status to %s", s)
		}
	})
	if _, at := tabOf(ids[0]); at == "" {
		t.Fatal("tab shows no observed submit")
	}

	// The person closes the second tab: sweep turns it into "Reopen and refill".
	b.closeTab(second.TabID)
	out, err := run(t, dbPath, true, "fill", "sweep")
	if err != nil {
		t.Fatalf("sweep: %v (%s)", err, out)
	}
	var sw applypark.SweepResult
	if err := json.Unmarshal([]byte(out), &sw); err != nil || len(sw.Lost) != 1 || sw.Lost[0] != ids[1] {
		t.Fatalf("sweep = %s (%v)", out, err)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		if s := statusOf(t, db, ids[1]); s != applications.StatusNeedsYou {
			t.Errorf("lost tab: %s", s)
		}
		if f, _ := applications.NewStore(db).GetFill(context.Background(), "default", ids[1]); !strings.Contains(f.Note, "Reopen and refill") {
			t.Errorf("note = %q", f.Note)
		}
	})

	// Resume: the parked tab is bound to the new task alone; the lost one gets a fresh fill.
	if out, err := run(t, dbPath, true, "fill", "resume", ids[0]); err != nil {
		t.Fatalf("resume: %v (%s)", err, out)
	}
	tab, at := tabOf(ids[0])
	if tab.State != applypark.StateParked && tab.State != applypark.StateActive {
		t.Fatalf("tab = %+v", tab)
	}
	if at != "" {
		t.Errorf("a resumed fill kept the old observed submit")
	}
	if _, err := run(t, dbPath, true, "fill", "resume", "nope"); err == nil {
		t.Error("resume of an unknown application")
	}
}

func TestFillResumeNeedsAParkedOrLostTab(t *testing.T) {
	dbPath := homeDB(t)
	ids := jobs(t, dbPath, 1)
	b := &browser{}
	b.install(t, dbPath)
	if out, err := run(t, dbPath, true, "fill", "resume", ids[0]); err == nil || !strings.Contains(err.Error(), "no parked or lost tab") {
		t.Fatalf("resume with no tab: %v (%s)", err, out)
	}
	if len(b.started) != 0 {
		t.Fatal("a refused resume started a task")
	}
}
