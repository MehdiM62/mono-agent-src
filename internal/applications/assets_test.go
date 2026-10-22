// internal/applications/assets_test.go
package applications_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/applications"
)

func TestStoreAwaitingSubmitAndNeedsYouTransitions(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)

	steps := []applications.Status{
		applications.StatusNeedsYou, applications.StatusAwaitingSubmit,
		applications.StatusApplied, applications.StatusRejected,
	}
	for _, to := range steps {
		actor := applications.ActorSystem
		if to == applications.StatusApplied || to == applications.StatusRejected {
			actor = applications.ActorUser
		}
		if err := store.SetStatus(ctx, "default", app.ID, to, actor, "step"); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
	log, err := store.StatusLog(ctx, "default", app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 5 || log[1].ToStatus != applications.StatusNeedsYou || log[2].FromStatus != "needs_you" {
		t.Fatalf("status history = %+v", log)
	}

	other := createTestJob(t, store)
	if err := store.SetStatus(ctx, "default", other.ID, applications.StatusAwaitingSubmit, applications.ActorSystem, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(ctx, "default", other.ID, applications.StatusRejected, applications.ActorUser, ""); !errors.Is(err, applications.ErrInvalidTransition) {
		t.Fatalf("awaiting_submit -> rejected = %v, want ErrInvalidTransition", err)
	}
	if err := store.SetStatus(ctx, "default", other.ID, applications.StatusApplied, applications.ActorSystem, ""); !errors.Is(err, applications.ErrInvalidTransition) {
		t.Fatalf("system -> applied = %v, want ErrInvalidTransition", err)
	}
	third := createTestJob(t, store)
	store.SetStatus(ctx, "default", third.ID, applications.StatusNeedsYou, applications.ActorSystem, "")
	for _, to := range []applications.Status{applications.StatusApplied, applications.StatusPending} {
		if err := store.SetStatus(ctx, "default", third.ID, to, applications.ActorUser, ""); !errors.Is(err, applications.ErrInvalidTransition) {
			t.Fatalf("needs_you -> %s = %v, want ErrInvalidTransition", to, err)
		}
	}
}

func TestConversationLinkIsOnePerApplication(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkConversation, "c1"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkConversation, "c1"); err != nil {
		t.Fatalf("same conversation again: %v", err)
	}
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkConversation, "c2"); !errors.Is(err, applications.ErrInvalidInput) {
		t.Fatalf("second conversation = %v", err)
	}
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkHIL, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO application_links (application_id, kind, ref_id, profile_id, created_at) VALUES (?, 'conversation', 'c3', 'default', 'x')`, app.ID); err == nil {
		t.Fatal("partial unique index did not stop a second conversation row")
	}
}

func TestLinksAddListRemoveAndValidate(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)

	for i := 0; i < 2; i++ { // second add is a no-op
		if err := store.AddLink(ctx, "default", app.ID, applications.LinkPerson, "p1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkMessage, "m1"); err != nil {
		t.Fatal(err)
	}
	all, _ := store.ListLinks(ctx, "default", app.ID, "")
	people, _ := store.ListLinks(ctx, "default", app.ID, applications.LinkPerson)
	if len(all) != 2 || len(people) != 1 || people[0].RefID != "p1" {
		t.Fatalf("all=%v people=%v", all, people)
	}
	if removed, err := store.RemoveLink(ctx, "default", app.ID, applications.LinkPerson, "p1"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if removed, _ := store.RemoveLink(ctx, "default", app.ID, applications.LinkPerson, "p1"); removed {
		t.Fatal("second remove reported a removal")
	}

	if err := store.AddLink(ctx, "default", app.ID, "bogus", "x"); !errors.Is(err, applications.ErrInvalidInput) {
		t.Fatalf("bad kind = %v", err)
	}
	if err := store.AddLink(ctx, "default", app.ID, applications.LinkTask, strings.Repeat("x", applications.MaxRefIDLen+1)); !errors.Is(err, applications.ErrInvalidInput) {
		t.Fatalf("long ref = %v", err)
	}
	if err := store.AddLink(ctx, "default", "nope", applications.LinkTask, "1"); !errors.Is(err, applications.ErrNotFound) {
		t.Fatalf("unknown app = %v", err)
	}
	if err := store.AddLink(ctx, "other-profile", app.ID, applications.LinkTask, "1"); !errors.Is(err, applications.ErrNotFound) {
		t.Fatalf("other profile = %v", err)
	}
}

func TestFillRecordRoundTripReplacesAndRedacts(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)

	if f, err := store.GetFill(ctx, "default", app.ID); err != nil || f.Status != applications.FillNone || len(f.Fields) != 0 {
		t.Fatalf("empty fill = %+v, %v", f, err)
	}
	err := store.RecordFill(ctx, "default", app.ID, applications.Fill{
		Status: applications.FillFilled, Screenshot: "/p/shot.png", Note: "ok",
		Fields: []applications.FillField{
			{Label: "Email", Value: "a@b.example", Source: "profile"},
			{Label: "Cover letter", Value: "Dear...", Source: "doc-001", Generated: true},
			{Label: "Account password", Value: "hunter2"},
			{Label: "Notes", Value: "sk-abcdef0123456789"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := store.GetFill(ctx, "default", app.ID)
	if f.Status != applications.FillFilled || len(f.Fields) != 4 || !f.Fields[1].Generated || f.UpdatedAt == "" {
		t.Fatalf("fill = %+v", f)
	}
	if f.Fields[2].Value != applications.RedactedValue || f.Fields[3].Value != applications.RedactedValue {
		t.Fatalf("secret-shaped values stored: %+v", f.Fields)
	}
	if f.Fields[0].Value != "a@b.example" {
		t.Fatalf("plain value changed: %+v", f.Fields[0])
	}

	if err := store.RecordFill(ctx, "default", app.ID, applications.Fill{Status: applications.FillNeedsYou, Note: "captcha"}); err != nil {
		t.Fatal(err)
	}
	f, _ = store.GetFill(ctx, "default", app.ID)
	if f.Status != applications.FillNeedsYou || len(f.Fields) != 0 {
		t.Fatalf("second record did not replace: %+v", f)
	}
}

func TestFillRecordRefusesOverCapAndBadStatus(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)

	cases := map[string]applications.Fill{
		"status":    {Status: "weird"},
		"note":      {Status: applications.FillFilled, Note: strings.Repeat("n", applications.MaxNoteLen+1)},
		"value":     {Status: applications.FillFilled, Fields: []applications.FillField{{Label: "a", Value: strings.Repeat("v", applications.MaxFieldValueLen+1)}}},
		"nolabel":   {Status: applications.FillFilled, Fields: []applications.FillField{{Value: "v"}}},
		"fieldlist": {Status: applications.FillFilled, Fields: make([]applications.FillField, applications.MaxFillFields+1)},
	}
	for name, fill := range cases {
		if err := store.RecordFill(ctx, "default", app.ID, fill); !errors.Is(err, applications.ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
	if f, _ := store.GetFill(ctx, "default", app.ID); f.Status != applications.FillNone {
		t.Fatalf("a refused record was stored: %+v", f)
	}
}

func TestMigrationKeepsExistingApplicationChildren(t *testing.T) {
	db := newTestDB(t)
	store := applications.NewStore(db.DB)
	ctx := context.Background()
	app := createTestJob(t, store)
	if err := store.AddTag(ctx, "default", app.ID, "urgent"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "default", app.ID)
	if err != nil || got.Job == nil || len(got.Tags) != 1 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	// Re-running migrations is a no-op and keeps the rows.
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "default", app.ID); err != nil {
		t.Fatal(err)
	}
}
