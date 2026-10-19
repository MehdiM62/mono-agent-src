package dynorg

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newAppAccessConductor(t *testing.T, ex *execScript, noAppAccess bool) *Conductor {
	t.Helper()
	c := New(context.Background(), Config{
		Cwd: "/w", Limits: Limits{MaxAgents: 6, MaxConcurrent: 3},
		Staffer:    &Staffer{Roster: []Model{opus, haiku}, Lead: opus, NoAppAccess: noAppAccess},
		ReadAccess: true, Exec: ex.exec, Emit: &recEmitter{},
	})
	t.Cleanup(c.Close)
	return c
}

// coder.appAccess off: no worker gets the workspace profile. The lead's
// explicit ask is refused, a picked one is lowered to coding, and neither the
// worker's prompt nor org_roster offers the workspace rules.
func TestAppAccessOffKeepsWorkspaceOutOfTheSpawn(t *testing.T) {
	ex := &execScript{}
	c := newAppAccessConductor(t, ex, true)
	if _, err := c.Spawn(context.Background(), SpawnRequest{Brief: "tidy my tasks", Access: ProfileWorkspace}); err == nil ||
		!strings.Contains(err.Error(), "app access off") || strings.Contains(err.Error(), "use one of workspace") {
		t.Fatalf("an explicit workspace spawn = %v, want refused", err)
	}
	info, err := c.Spawn(context.Background(), SpawnRequest{Brief: "tidy my tasks", Role: "task-manager", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Access != ProfileCoding {
		t.Errorf("task-manager access = %s, want coding", info.Access)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("%d workers ran, want 1", len(ex.calls))
	}
	p := ex.calls[0].SystemPrompt
	if strings.Contains(p, workspaceRules) || strings.Contains(p, sendRule) {
		t.Errorf("the worker's prompt keeps the workspace rules:\n%s", p)
	}
	if !strings.Contains(p, "Never read, send or change the user's messages") {
		t.Errorf("the worker's prompt lacks the no-messages rule:\n%s", p)
	}
	v := c.Roster()
	if _, ok := v.Profiles[ProfileWorkspace]; ok || len(v.Profiles) != len(Profiles)-1 {
		t.Errorf("org_roster access profiles = %v", v.Profiles)
	}
	b, _ := json.Marshal(v.Roles)
	if strings.Contains(string(b), `"default_access":"workspace"`) {
		t.Errorf("org_roster still defaults roles to workspace: %s", b)
	}
}

// On (the default), the same spawn gets workspace and its rules.
func TestAppAccessOnStaffsWorkspace(t *testing.T) {
	ex := &execScript{}
	c := newAppAccessConductor(t, ex, false)
	info, err := c.Spawn(context.Background(), SpawnRequest{Brief: "tidy my tasks", Role: "task-manager", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Access != ProfileWorkspace || !strings.Contains(ex.calls[0].SystemPrompt, workspaceRules) || !strings.Contains(ex.calls[0].SystemPrompt, sendRule) {
		t.Errorf("access = %s, prompt:\n%s", info.Access, ex.calls[0].SystemPrompt)
	}
	if _, ok := c.Roster().Profiles[ProfileWorkspace]; !ok {
		t.Error("org_roster lacks workspace")
	}
}

func TestLeadPromptWithoutAppAccess(t *testing.T) {
	on, off := LeadPrompt(DefaultLimits(), true), LeadPrompt(DefaultLimits(), false)
	for _, rule := range []string{workspaceRules, sendRule, "(workspace, coding"} {
		if !strings.Contains(on, rule) {
			t.Errorf("app access on: the lead prompt misses %q", rule)
		}
		if strings.Contains(off, rule) {
			t.Errorf("app access off: the lead prompt keeps %q", rule)
		}
	}
	if !strings.Contains(off, "(coding, qa, automation, research)") {
		t.Errorf("app access off: profiles listed wrong:\n%s", off)
	}
}

// A veteran from a turn that had app access can't go on as workspace.
func TestAppAccessOffBlocksAWorkspaceVeteran(t *testing.T) {
	ex := &execScript{}
	c := newAppAccessConductor(t, ex, true)
	c.AddVeterans([]Veteran{{ID: "w1", Role: "Task Manager", AgentType: "task-manager", Access: ProfileWorkspace,
		Runtime: "claude", Model: "opus", Session: "s1", Report: "done before"}})
	if _, err := c.Message(context.Background(), "w1", "go on"); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Errorf("org_message to a workspace veteran = %v, want refused", err)
	}
	if r := c.Roster(); len(r.Workers) != 1 || !strings.Contains(r.Workers[0].Error, "app access") {
		t.Errorf("roster workers = %+v", r.Workers)
	}
}

// The workers' profile section is built once per turn, not at every spawn,
// and org_roster has it rebuilt for the next spawn.
func TestWorkersProfileSectionIsCachedPerTurn(t *testing.T) {
	var builds atomic.Int32
	ProfileContext = func(context.Context) string {
		return "Profile: Work (build " + string(rune('0'+builds.Add(1))) + ")"
	}
	defer func() { ProfileContext = nil }()
	ex := &execScript{}
	c := newAppAccessConductor(t, ex, false)
	ctx := context.Background()
	for _, brief := range []string{"implement a", "implement b"} {
		if _, err := c.Spawn(ctx, SpawnRequest{Brief: brief, Wait: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Message(ctx, "w1", "and also c"); err != nil {
		t.Fatal(err)
	}
	c.Wait(ctx, nil, 5*time.Second)
	if n := builds.Load(); n != 1 {
		t.Fatalf("profile section built %d times in a turn, want 1", n)
	}
	if _, err := c.Handle(ctx, ToolRoster, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if n := builds.Load(); n != 1 {
		t.Errorf("org_roster built it itself (%d); the next spawn does", n)
	}
	if _, err := c.Spawn(ctx, SpawnRequest{Brief: "implement d", Wait: true}); err != nil {
		t.Fatal(err)
	}
	if n := builds.Load(); n != 2 {
		t.Fatalf("after org_roster: built %d times, want 2", n)
	}
	for i, o := range ex.calls {
		want := "(build 1)"
		if i == len(ex.calls)-1 {
			want = "(build 2)"
		}
		if strings.Count(o.SystemPrompt, "## Profile context") != 1 || !strings.Contains(o.SystemPrompt, want) {
			t.Errorf("worker run %d prompt has the wrong profile section (want %s):\n%s", i, want, o.SystemPrompt)
		}
	}
}
