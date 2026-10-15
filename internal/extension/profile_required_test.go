package extension

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/capture"
	"github.com/monoes/mono-agent/internal/recording"
)

// The profile gate is enforced in the CLI, so a method that acts on a
// profile and was given none must be refused here, in plain words, before
// any CLI is run.

func wantChooseProfile(t *testing.T, what string, err error) {
	t.Helper()
	var re *RequestError
	if !asRequestError(err, &re) || re.Code != CodeInvalidInput || err.Error() != chooseProfileMessage {
		t.Fatalf("%s: err = %v, want invalid_input %q", what, err, chooseProfileMessage)
	}
}

func TestChatMethodsRefuseARequestWithoutAProfile(t *testing.T) {
	f := &fakeChat{}
	srv := chatSrv(f)
	_, err := callChat(t, srv, MethodChatSend, map[string]any{"conversation": "c1", "message": "hi"}, nil)
	wantChooseProfile(t, "chat.send", err)
	_, err = callChat(t, srv, MethodChatEvents, map[string]any{"conversation": "c1"}, nil)
	wantChooseProfile(t, "chat.events", err)
	if len(f.specs) != 0 || len(f.created) != 0 || f.listArgs != nil {
		t.Fatalf("the backend ran: specs=%v created=%v list=%v", f.specs, f.created, f.listArgs)
	}
}

func TestChatMethodsPassTheProfileOn(t *testing.T) {
	f := &fakeChat{}
	srv := chatSrv(f)
	if _, err := callChat(t, srv, MethodChatEvents, map[string]any{"profile": "p-work", "conversation": "c1"}, nil); err != nil {
		t.Fatal(err)
	}
	if f.listArgs[0] != "p-work" {
		t.Fatalf("events profile = %v", f.listArgs[0])
	}
}

func TestFormFillRefusesARequestWithoutAProfile(t *testing.T) {
	f := &fakeFiller{}
	_, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"fields": []any{fld("first", "First Name", "text", nil)},
	}, nil)
	wantChooseProfile(t, "form.fill", err)
	if len(f.queries) != 0 || f.prompt != "" {
		t.Fatal("form.fill ran without a profile")
	}
}

func TestChatBaseNeverOmitsTheProfile(t *testing.T) {
	if got := strings.Join(chatBase("p-work"), " "); got != "--profile=p-work --json" {
		t.Fatalf("chatBase = %q", got)
	}
}

func TestRecordMethodsRefuseARequestWithoutAProfile(t *testing.T) {
	srv, ext, _ := startCaptureServer(t)
	drafts, _ := recording.DraftsDir()
	if err := os.MkdirAll(filepath.Join(drafts, "rec-1"), 0o700); err != nil {
		t.Fatal(err)
	}
	run := &argvRunner{out: `{}`}
	srv.SetRecordRunner(run)
	for _, c := range []struct {
		method string
		params map[string]any
	}{
		{MethodRecordList, map[string]any{}},
		{MethodRecordAnalyze, map[string]any{"recordingId": "rec-x"}},
		{MethodRecordVerify, map[string]any{"draftDir": "rec-1"}},
		{MethodRecordSave, map[string]any{"draftDir": "rec-1"}},
	} {
		ext.ask("q", c.method, c.params)
		reply := ext.settled()
		if reply.OK || reply.Code != CodeInvalidInput || reply.Error != chooseProfileMessage {
			t.Fatalf("%s: reply = %+v", c.method, reply)
		}
	}
	if run.args != nil {
		t.Fatalf("the CLI ran: %v", run.args)
	}
}

// record.save is never sent a profile by the side panel: the draft knows it.
func TestRecordSaveTakesTheProfileFromTheDraft(t *testing.T) {
	srv, ext, _ := startCaptureServer(t)
	drafts, _ := recording.DraftsDir()
	draft := filepath.Join(drafts, "rec-1")
	if err := os.MkdirAll(draft, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := `{"source":"` + recording.SourceRecording + `","url":"https://example.com/","profile":"p-work"}`
	if err := os.WriteFile(filepath.Join(draft, capture.MetaFile), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	run := &argvRunner{out: `{"ok":true}`}
	srv.SetRecordRunner(run)

	for _, method := range []string{MethodRecordSave, MethodRecordVerify} {
		run.args = nil
		ext.ask("s1", method, map[string]any{"draftDir": "rec-1"})
		if reply := ext.settled(); !reply.OK {
			t.Fatalf("%s: %+v", method, reply)
		}
		if len(run.args) == 0 || run.args[0] != "--profile=p-work" {
			t.Fatalf("%s argv = %v, want the draft's profile", method, run.args)
		}
	}

	// A profile in the request wins over the draft's.
	run.args = nil
	ext.ask("s2", MethodRecordSave, map[string]any{"draftDir": "rec-1", "profile": "p-other"})
	if reply := ext.settled(); !reply.OK || run.args[0] != "--profile=p-other" {
		t.Fatalf("argv = %v, reply = %+v", run.args, reply)
	}
}

func TestGateRefusalBecomesTheChooseProfileAnswer(t *testing.T) {
	gate := "this command acts on a profile: pass --profile <id|name>. Profiles: Work (p-work)"
	exit := errors.New("exit status 3")

	// chat: the refusal is on stderr, or in the JSON the CLI prints.
	err := cliFailure(nil, gate, exit)
	wantChooseProfile(t, "chat stderr", err)
	err = cliFailure([]byte(`{"error":"`+gate+`","code":"invalid_input"}`), "", exit)
	wantChooseProfile(t, "chat json", err)
	if err := cliFailure(nil, "boom", exit); err.Error() != "boom" {
		t.Fatalf("other failures keep their text, got %v", err)
	}

	// record: same.
	args := []string{"record", "list", "--json"}
	wantChooseProfile(t, "record stderr", recordCommandFailure(args, "", gate, exit))
	wantChooseProfile(t, "record stdout", recordCommandFailure(args, gate, "", exit))
	if err := recordCommandFailure(args, "", "boom", exit); !strings.Contains(err.Error(), "boom") || err.Error() == chooseProfileMessage {
		t.Fatalf("other failures keep their text, got %v", err)
	}
}

// A capture is never lost for want of a profile (a queued one made while
// the bridge was down must land), but the shared inbox is said out loud.
func TestCaptureWithoutAProfileIsWarnedAbout(t *testing.T) {
	var logs bytes.Buffer
	srv := NewServer("127.0.0.1:0", zerolog.New(&logs))
	var hooked int
	srv.SetAfterWrite(func(*capture.Result) { hooked++ })

	srv.written(&capture.Result{Path: "/inbox/2026-10-15-x"})
	if hooked != 1 {
		t.Fatalf("after-write hook ran %d times, want 1: the capture is still accepted", hooked)
	}
	out := logs.String()
	if !strings.Contains(out, `"level":"warn"`) || !strings.Contains(out, "shared inbox") || !strings.Contains(out, "/inbox/2026-10-15-x") {
		t.Fatalf("log = %q", out)
	}

	logs.Reset()
	srv.written(&capture.Result{Path: "/p/x", Meta: capture.Meta{Profile: "p-work"}})
	if logs.Len() != 0 {
		t.Fatalf("a profiled capture was warned about: %q", logs.String())
	}
}
