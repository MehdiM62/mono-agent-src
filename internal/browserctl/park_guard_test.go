package browserctl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/applications"
	"github.com/monoes/mono-agent/internal/applypark"
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/testdb"
)

// A tab bound in application_tabs is refused to agents: parked to every
// agent, active to every task but the one it is bound to. A human still
// drives it, and an agent's own other tab is untouched.
func TestParkedTabIsRefusedToAgentsAndActiveOnlyToItsTask(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	app := &applications.Application{ProfileID: "default", Kind: applications.KindJob,
		Job: &applications.JobDetails{Title: "E", Company: "A", URL: "https://jobs.example/1"}}
	if err := applications.NewStore(db).Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	f := browserctltest.New()
	parked := f.AddTab("https://jobs.example/apply", "Coder · Fill A", true)
	other := f.AddTab("http://localhost:3000/", "Coder · Fill B", false)
	st := applypark.Store{DB: db, Profile: "default"}
	if err := st.RecordOpen(ctx, "at-A", parked); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(ctx, applypark.BindRequest{ApplicationID: app.ID, TaskID: "at-A", TabID: parked, Origin: "https://jobs.example",
		Live: []applypark.LiveTab{{TabID: parked, GroupTitle: "Coder · Fill A", URL: "https://jobs.example/apply"}}}); err != nil {
		t.Fatal(err)
	}
	p := browserctl.DefaultPermissions()
	p.SetSite("https://jobs.example", browserctl.RuleAllow)
	open := func(tab int) error {
		_, err := browserctl.Open(browserctl.Options{Profile: "default", Sender: f, Permissions: &p, DB: db, Tab: tab})
		return err
	}

	var pe *browserctl.ParkedTabError
	// Parked: refused to the apply agent, whichever task it is.
	browserctltest.AsApplyTask(t, "at-A")
	if err := open(parked); !errors.As(err, &pe) || pe.ApplicationID != app.ID {
		t.Fatalf("parked tab, its own task: %v", err)
	}
	browserctltest.AsApplyTask(t, "at-B")
	if err := open(parked); !errors.As(err, &pe) {
		t.Fatalf("parked tab, another task: %v", err)
	}
	// Without a default tab the context's tab is not silently the parked one either.
	if err := open(0); errors.As(err, &pe) && pe.TabID == parked {
		t.Fatalf("the default tab was the parked one")
	}

	// Resumed: active for task at-A only.
	if err := st.SetState(ctx, app.ID, applypark.StateActive, "at-A"); err != nil {
		t.Fatal(err)
	}
	browserctltest.AsApplyTask(t, "at-B")
	if err := open(parked); !errors.As(err, &pe) || pe.State != "active" {
		t.Fatalf("active tab, another task: %v", err)
	}
	browserctltest.AsAgentKind(t, browserctl.KindApply) // an apply agent with no task
	if err := open(parked); !errors.As(err, &pe) {
		t.Fatalf("active tab, no task: %v", err)
	}
	browserctltest.AsApplyTask(t, "at-A")
	if err := open(parked); err != nil {
		t.Fatalf("active tab, its own task: %v", err)
	}

	// A closed binding frees the tab; an agent's unrelated tab was never affected.
	if err := st.SetState(ctx, app.ID, applypark.StateClosed, ""); err != nil {
		t.Fatal(err)
	}
	browserctltest.AsApplyTask(t, "at-B")
	if err := open(parked); err != nil {
		t.Fatalf("closed binding still refuses: %v", err)
	}
	if err := open(other); err != nil {
		t.Fatalf("an unbound tab was refused: %v", err)
	}
}

func TestAHumanIsNeverRefusedAParkedTab(t *testing.T) {
	setEnv(t, false)
	ctx := context.Background()
	db := testdb.Open(t).DB
	app := &applications.Application{ProfileID: "default", Kind: applications.KindJob,
		Job: &applications.JobDetails{Title: "E", Company: "A", URL: "https://jobs.example/1"}}
	if err := applications.NewStore(db).Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	f := browserctltest.New()
	tab := f.AddTab("https://jobs.example/apply", "Coder · Fill A", true)
	if err := (applypark.Store{DB: db, Profile: "default"}).RecordOpen(ctx, "at-A", tab); err != nil {
		t.Fatal(err)
	}
	if err := (applypark.Store{DB: db, Profile: "default"}).Bind(ctx, applypark.BindRequest{ApplicationID: app.ID, TaskID: "at-A", TabID: tab,
		Origin: "https://jobs.example", Live: []applypark.LiveTab{{TabID: tab, GroupTitle: "Coder · Fill A", URL: "https://jobs.example/apply"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := browserctl.Open(browserctl.Options{Profile: "default", Sender: f, DB: db, Tab: tab}); err != nil {
		t.Fatalf("the person was refused their own tab: %v", err)
	}
}

// `browser open` by an apply task records the tab as that task's, taken from
// the verified token; another kind of agent records nothing.
func TestOpenByAnApplyTaskRecordsTheTab(t *testing.T) {
	setEnv(t, true)
	browserctltest.AsApplyTask(t, "at-A")
	db := testdb.Open(t).DB
	f := browserctltest.New()
	p := browserctl.DefaultPermissions()
	s, err := browserctl.Open(browserctl.Options{Profile: "default", NoTab: true, Sender: f, Permissions: &p, DB: db})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.OpenTab("http://localhost:3000/", "")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM application_tab_opens WHERE task_id = 'at-A' AND tab_id = ?`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("recorded %d opens for tab %d (%v)", n, id, err)
	}

	setEnv(t, true)
	browserctltest.AsAgentKind(t, browserctl.KindCoder)
	s, err = browserctl.Open(browserctl.Options{Profile: "default", NoTab: true, Sender: f, Permissions: &p, DB: db})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.OpenTab("http://localhost:3000/", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM application_tab_opens WHERE tab_id = ?`, id2).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a coder's open was recorded: %d (%v)", n, err)
	}
}
