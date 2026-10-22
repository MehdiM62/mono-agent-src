package ai

import (
	"errors"
	"testing"
)

// The old modes are aliases: assistant is scoped, coder is full, and Mode
// follows the level on every row.
func TestConversationAccessAndModeStayInStep(t *testing.T) {
	s, _ := newChatEventStore(t)
	a, err := s.CreateConversationModeEffort("default", "agent", "general", "claude", "", "", ModeAssistant, "", "")
	if err != nil || a.Access != AccessScoped || a.Mode != ModeAssistant {
		t.Fatalf("assistant: %+v %v", a, err)
	}
	c, err := s.CreateConversationModeEffort("default", "agent", "general", "claude", "", "", ModeCoder, t.TempDir(), "")
	if err != nil || c.Access != AccessFull || c.Mode != ModeCoder {
		t.Fatalf("coder: %+v %v", c, err)
	}
	got, err := s.GetConversation(c.ID, "default")
	if err != nil || got.Access != AccessFull || got.Record().Access != AccessFull {
		t.Fatalf("stored: %+v %v", got, err)
	}
	for _, lvl := range []string{AccessCoding, AccessWorkspace} {
		cv, err := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", lvl, t.TempDir(), "")
		if err != nil || cv.Mode != ModeCoder || cv.Access != lvl {
			t.Fatalf("%s: %+v %v", lvl, cv, err)
		}
	}
	if _, err := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", AccessCoding, "", ""); err == nil {
		t.Error("a coding conversation was created without a folder")
	}
	if _, err := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", "root", "", ""); err == nil {
		t.Error("an unknown level was accepted")
	}
	if got := (Conversation{Mode: ModeCoder}).EffectiveAccess(); got != AccessFull {
		t.Errorf("a row without access derives %q from its mode", got)
	}
}

// Moving a level keeps mode in step, leaves a fresh session when the
// conversation enters or leaves scoped, and a downgrade to scoped puts the
// org back to solo.
func TestSetConversationAccess(t *testing.T) {
	s, _ := newChatEventStore(t)
	dir := t.TempDir()
	c, _ := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", AccessFull, dir, "")
	if err := s.BindConversationSession(c.ID, "default", "claude", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConversationOrgMode(c.ID, "default", OrgModeDynamic); err != nil {
		t.Fatal(err)
	}
	same, err := s.SetConversationAccess(c.ID, "default", AccessCoding, "")
	if err != nil || same.Access != AccessCoding || same.Mode != ModeCoder || same.SessionID != "sess-1" || same.OrgMode != OrgModeDynamic {
		t.Fatalf("full -> coding: %+v %v", same, err)
	}
	down, err := s.SetConversationAccess(c.ID, "default", AccessScoped, "")
	if err != nil || down.Mode != ModeAssistant || down.SessionID != "" || down.OrgMode != OrgModeSolo || down.Cwd != dir {
		t.Fatalf("coding -> scoped: %+v %v", down, err)
	}
	up, err := s.SetConversationAccess(c.ID, "default", AccessWorkspace, "")
	if err != nil || up.Mode != ModeCoder || up.Cwd != dir {
		t.Fatalf("scoped -> workspace: %+v %v", up, err)
	}
	fresh, _ := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", AccessScoped, "", "")
	if _, err := s.SetConversationAccess(fresh.ID, "default", AccessCoding, ""); err == nil {
		t.Error("a scoped conversation with no folder was raised without one")
	}
	if _, err := s.SetConversationAccess(c.ID, "other", AccessScoped, ""); err == nil {
		t.Error("another profile moved the conversation")
	}
}

// The expected-level move is one conditional UPDATE: a row that changed since
// the caller read it is refused and nothing else about it is touched.
func TestSetConversationAccessIfRefusesAChangedRow(t *testing.T) {
	s, _ := newChatEventStore(t)
	c, _ := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", AccessFull, t.TempDir(), "")
	if err := s.BindConversationSession(c.ID, "default", "claude", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConversationOrgMode(c.ID, "default", OrgModeDynamic); err != nil {
		t.Fatal(err)
	}
	// The caller read "full"; another process lowers it to coding first.
	if _, err := s.SetConversationAccess(c.ID, "default", AccessCoding, ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.SetConversationAccessIf(c.ID, "default", AccessScoped, "", AccessFull)
	var changed *AccessChangedError
	if !errors.As(err, &changed) || changed.Current != AccessCoding {
		t.Fatalf("err = %v", err)
	}
	got, _ := s.GetConversation(c.ID, "default")
	if got.Access != AccessCoding || got.SessionID != "sess-1" || got.OrgMode != OrgModeDynamic || got.Mode != ModeCoder {
		t.Errorf("a refused move changed the row: %+v", got)
	}
	// With the level it expects, it applies like the unconditional one.
	down, err := s.SetConversationAccessIf(c.ID, "default", AccessScoped, "", AccessCoding)
	if err != nil || down.Access != AccessScoped || down.Mode != ModeAssistant || down.SessionID != "" || down.OrgMode != OrgModeSolo {
		t.Fatalf("expected move: %+v %v", down, err)
	}
	fresh, _ := s.CreateConversationAccess("default", "agent", "general", "claude", "", "", AccessScoped, "", "")
	if _, err := s.SetConversationAccessIf(fresh.ID, "default", AccessCoding, "", AccessScoped); err == nil || errors.As(err, &changed) {
		t.Errorf("a scoped row with no folder was raised from nowhere, or reported as changed: %v", err)
	}
	up, err := s.SetConversationAccessIf(fresh.ID, "default", AccessWorkspace, t.TempDir(), AccessScoped)
	if err != nil || up.Access != AccessWorkspace || up.Cwd == "" {
		t.Fatalf("raise with a folder: %+v %v", up, err)
	}
}
