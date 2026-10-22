package applyfill

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/applications"
	"github.com/monoes/mono-agent/internal/applypark"
)

func withTabs(t *testing.T, tabs []applypark.LiveTab, err error) {
	t.Helper()
	old := Parking
	t.Cleanup(func() { Parking = old })
	Parking = &ParkingConfig{Live: func(context.Context, string) ([]applypark.LiveTab, error) { return tabs, err }}
}

func agentTab(id int, url string) applypark.LiveTab {
	return applypark.LiveTab{TabID: id, GroupTitle: "Coder · Fill", URL: url}
}

// opened notes that the fixture task opened tab, as `browser open` does.
func opened(t *testing.T, db *sql.DB, tab int) {
	t.Helper()
	if err := (applypark.Store{DB: db, Profile: profile}).RecordOpen(bg, "at-0123456789abcdef", tab); err != nil {
		t.Fatal(err)
	}
}

func filled(app *applications.Application, tab int, origin string) Result {
	return Result{Status: StatusFilled, Fields: []Field{{Label: "Email", Value: "a@b.example", Source: "Profile info"}}, TabID: tab, Origin: origin}
}

func TestSchemaTakesAnOptionalTabAndOrigin(t *testing.T) {
	ok := func(v any) error { return aitask.Validate(Schema, mustDecode(mustJSON(v))) }
	if err := ok(map[string]any{"status": "filled", "fields": []any{}, "tabId": 12, "origin": "https://jobs.example"}); err != nil {
		t.Errorf("with a tab: %v", err)
	}
	for name, v := range map[string]any{
		"tab a string": map[string]any{"status": "filled", "fields": []any{}, "tabId": "12"},
		"tab a float":  map[string]any{"status": "filled", "fields": []any{}, "tabId": 1.5},
	} {
		if ok(v) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFinalizeParksTheTabTheBrowserConfirms(t *testing.T) {
	db, _ := fixture(t)
	app := newApp(t, db)
	withTabs(t, []applypark.LiveTab{agentTab(12, "https://jobs.example/apply")}, nil)
	opened(t, db, 12)
	if err := Finalize(bg, db, profile, completed(t, app, filled(app, 12, "https://jobs.example"))); err != nil {
		t.Fatal(err)
	}
	tab, _ := applypark.Store{DB: db, Profile: profile}.Get(bg, app.ID)
	if tab == nil || tab.TabID != 12 || tab.State != applypark.StateParked || tab.TaskID != "at-0123456789abcdef" {
		t.Fatalf("tab = %+v", tab)
	}
	if got := status(t, db, app.ID); got != applications.StatusAwaitingSubmit {
		t.Fatalf("status = %s", got)
	}
}

func TestFinalizeBindsNothingTheBrowserDoesNotConfirm(t *testing.T) {
	cases := map[string]struct {
		tabs   []applypark.LiveTab
		err    error
		tab    int
		origin string
		parked bool
	}{
		"tab not open":      {tabs: nil, tab: 12, origin: "https://jobs.example"},
		"other origin":      {tabs: []applypark.LiveTab{agentTab(12, "https://evil.example/")}, tab: 12, origin: "https://jobs.example"},
		"someone's tab":     {tabs: []applypark.LiveTab{{TabID: 12, GroupTitle: "Work", URL: "https://jobs.example/"}}, tab: 12, origin: "https://jobs.example"},
		"bad origin":        {tabs: []applypark.LiveTab{agentTab(12, "https://jobs.example/")}, tab: 12, origin: "file:///etc/passwd"},
		"origin only":       {tabs: []applypark.LiveTab{agentTab(12, "https://jobs.example/")}, tab: 0, origin: "https://jobs.example"},
		"browser unreached": {err: errors.New("no bridge"), tab: 12, origin: "https://jobs.example"},
	}
	for name, c := range cases {
		db, _ := fixture(t)
		app := newApp(t, db)
		withTabs(t, c.tabs, c.err)
		opened(t, db, 12)
		if err := Finalize(bg, db, profile, completed(t, app, filled(app, c.tab, c.origin))); err != nil {
			t.Fatalf("%s: a refused tab failed the whole fill: %v", name, err)
		}
		if tab, _ := (applypark.Store{DB: db, Profile: profile}).Get(bg, app.ID); tab != nil {
			t.Errorf("%s: bound %+v", name, tab)
		}
		fill, _ := applications.NewStore(db).GetFill(bg, profile, app.ID)
		if fill.Status != applications.FillFilled || !strings.Contains(fill.Note, "not tracked") {
			t.Errorf("%s: fill = %+v", name, fill)
		}
		if got := status(t, db, app.ID); got != applications.StatusAwaitingSubmit {
			t.Errorf("%s: status = %s", name, got)
		}
	}
}

func TestFinalizeWithoutAReaderBindsNothing(t *testing.T) {
	db, _ := fixture(t)
	app := newApp(t, db)
	old := Parking
	t.Cleanup(func() { Parking = old })
	Parking = nil
	if err := Finalize(bg, db, profile, completed(t, app, filled(app, 12, "https://jobs.example"))); err != nil {
		t.Fatal(err)
	}
	if tab, _ := (applypark.Store{DB: db, Profile: profile}).Get(bg, app.ID); tab != nil {
		t.Fatalf("bound %+v with no way to check it", tab)
	}
}

func TestFinalizeDoesNotParkAnOverApplicationsTab(t *testing.T) {
	db, _ := fixture(t)
	app := newApp(t, db)
	if err := applications.NewStore(db).SetStatus(bg, profile, app.ID, applications.StatusApplied, applications.ActorUser, "sent"); err != nil {
		t.Fatal(err)
	}
	withTabs(t, []applypark.LiveTab{agentTab(12, "https://jobs.example/apply")}, nil)
	opened(t, db, 12)
	if err := Finalize(bg, db, profile, completed(t, app, filled(app, 12, "https://jobs.example"))); err != nil {
		t.Fatal(err)
	}
	if tab, _ := (applypark.Store{DB: db, Profile: profile}).Get(bg, app.ID); tab != nil {
		t.Fatalf("a sent application got a parked tab: %+v", tab)
	}
	if got := status(t, db, app.ID); got != applications.StatusApplied {
		t.Fatalf("status = %s", got)
	}
}

func TestFinalizeWithATabNeverSetsApplied(t *testing.T) {
	db, _ := fixture(t)
	app := newApp(t, db)
	withTabs(t, []applypark.LiveTab{agentTab(12, "https://jobs.example/apply")}, nil)
	opened(t, db, 12)
	task := completed(t, app, filled(app, 12, "https://jobs.example"))
	for i := 0; i < 2; i++ {
		if err := Finalize(bg, db, profile, task); err != nil {
			t.Fatal(err)
		}
	}
	if got := status(t, db, app.ID); got == applications.StatusApplied {
		t.Fatal("a fill with a parked tab set applied")
	}
}

// A fill result naming a tab that another task opened (or none did) is not
// believed: nothing is bound and the other task's binding is untouched.
func TestFinalizeRefusesATabAnotherTaskOpened(t *testing.T) {
	db, _ := fixture(t)
	victim := newApp(t, db)
	thief := newApp(t, db)
	withTabs(t, []applypark.LiveTab{agentTab(12, "https://jobs.example/apply")}, nil)
	st := applypark.Store{DB: db, Profile: profile}
	if err := st.RecordOpen(bg, "at-victim", 12); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(bg, applypark.BindRequest{ApplicationID: victim.ID, TaskID: "at-victim", TabID: 12, Origin: "https://jobs.example",
		Live: []applypark.LiveTab{agentTab(12, "https://jobs.example/apply")}}); err != nil {
		t.Fatal(err)
	}
	// The thief's task ("at-0123456789abcdef") returns the victim's tab id and origin.
	if err := Finalize(bg, db, profile, completed(t, thief, filled(thief, 12, "https://jobs.example"))); err != nil {
		t.Fatal(err)
	}
	if tab, _ := st.Get(bg, thief.ID); tab != nil {
		t.Fatalf("the thief got a binding: %+v", tab)
	}
	if tab, _ := st.Get(bg, victim.ID); tab == nil || tab.TaskID != "at-victim" || tab.TabID != 12 {
		t.Fatalf("the victim's binding changed: %+v", tab)
	}
	fill, _ := applications.NewStore(db).GetFill(bg, profile, thief.ID)
	if !strings.Contains(fill.Note, "not tracked") {
		t.Fatalf("the refusal is not on the fill record: %q", fill.Note)
	}
}
