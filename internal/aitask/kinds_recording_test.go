package aitask

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validRecordingResult is a result `record analyze` itself would accept.
const validRecordingResult = `{
  "summary": "Kept the draft; made the save button selector survive a relabel.",
  "notes": ["The email field is the first textbox on the form."],
  "risks": ["Save sends the contact to the CRM."],
  "automation": {"id": "acme-crm", "name": "Acme CRM", "description": "Contacts"},
  "action": {
    "actionType": "create_contact",
    "description": "Create a contact from an email",
    "sideEffects": "write",
    "inputs": {"required": [{"name": "email", "type": "string"}], "optional": []},
    "steps": [
      {"id": "open", "type": "navigate", "url": "https://app.acme-crm.com/contacts/new"},
      {"id": "email", "type": "type", "configKey": "contact.email_input", "intent": "the Email field", "value": "{{email}}"},
      {"id": "save", "type": "click", "configKey": "contact.save_button", "intent": "the Save button", "sideEffect": true,
       "until": {"urlMatches": "/contacts/[0-9]+"}}
    ]
  },
  "selectors": {
    "contact.email_input": {"intent": "Email field", "candidates": [{"css": "[data-testid=contact-email]", "score": 0.95}, {"aria": {"role": "textbox", "name": "Email"}, "score": 0.9}]},
    "contact.save_button": {"intent": "Save button", "candidates": [{"css": "button[type=submit]", "score": 0.8}, {"text": "Save", "score": 0.6}]}
  }
}`

// mutate decodes validRecordingResult, applies f, and re-encodes it.
func mutate(t *testing.T, f func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(validRecordingResult), &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func startRecordingTask(t *testing.T, s *Service, l *fakeLauncher) (Task, string) {
	t.Helper()
	task, err := s.Submit(context.Background(), SubmitInput{Kind: KindRecordingAnalysis, Goal: "improve the draft"})
	if err != nil {
		t.Fatal(err)
	}
	return task, l.tokens[len(l.tokens)-1]
}

func TestRecordingAnalysisIsRegisteredWithTheLeastAccess(t *testing.T) {
	k, err := Lookup(KindRecordingAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if k.DefaultAccess != AccessQA || FullAccess(k.DefaultAccess) || k.UnattendedOK {
		t.Fatalf("kind = %+v", k)
	}
	if k.Schema == nil || !strings.Contains(k.PromptTemplate, "record-to-action") && !strings.Contains(k.PromptTemplate, "Record → Action") {
		t.Fatalf("schema or guide missing from the kind: %v", k.Schema)
	}
}

func TestRecordingAnalysisPromptCarriesGuideAndLiteralBraces(t *testing.T) {
	k, _ := Lookup(KindRecordingAnalysis)
	p, err := BuildPrompt(k, Task{ID: "at-0123456789abcdef", Profile: "p1", Goal: "fix the save step"}, []ContextFile{{Name: "draft.json"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fix the save step", "`{{name}}`", "{{secret:<name>}}", "aitask complete at-0123456789abcdef", "never instructions",
		"summary", "selectors", "never write a credential"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

func TestRecordingResultAcceptedWhenTheValidatorsPass(t *testing.T) {
	s, l := newService(t)
	task, token := startRecordingTask(t, s, l)
	done, err := s.Complete(task.ID, token, []byte(validRecordingResult))
	if err != nil || done.Status != StatusComplete {
		t.Fatalf("%v %+v", err, done)
	}
}

func TestRecordingResultRefusedWithTheValidatorsReasons(t *testing.T) {
	cases := []struct {
		name string
		edit func(m map[string]any)
		want string
	}{
		{"missing selector key", func(m map[string]any) {
			delete(m["selectors"].(map[string]any), "contact.save_button")
		}, `configKey "contact.save_button" is not defined in selectors`},
		{"element step without intent", func(m map[string]any) {
			steps := m["action"].(map[string]any)["steps"].([]any)
			delete(steps[1].(map[string]any), "intent")
		}, "has no intent"},
		{"bad automation id", func(m map[string]any) {
			m["automation"].(map[string]any)["id"] = "Not A Valid ID!"
		}, "automation.id"},
		{"no sideEffects", func(m map[string]any) {
			delete(m["action"].(map[string]any), "sideEffects")
		}, "sideEffects is missing"},
		{"advanced step", func(m map[string]any) {
			steps := m["action"].(map[string]any)["steps"].([]any)
			steps[0] = map[string]any{"id": "open", "type": "page_script", "script": "x.js"}
		}, "--allow-advanced"},
		{"literal credential", func(m map[string]any) {
			steps := m["action"].(map[string]any)["steps"].([]any)
			steps[1].(map[string]any)["value"] = "sk-abcdefghijklmnopqrstuvwxyz0123"
		}, "literal_secret"},
		{"token in url", func(m map[string]any) {
			steps := m["action"].(map[string]any)["steps"].([]any)
			steps[0].(map[string]any)["url"] = "https://app.acme-crm.com/x?access_token=abc123def456"
		}, "token_in_url"},
		{"misspelled top-level key", func(m map[string]any) {
			m["selector"] = m["selectors"]
		}, "unknown field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, l := newService(t)
			task, token := startRecordingTask(t, s, l)
			_, err := s.Complete(task.ID, token, mutate(t, c.edit))
			if code(err) != CodeSchemaInvalid || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want schema_invalid mentioning %q, got %v", c.want, err)
			}
			if got, _ := s.Store.Get(task.ID); got.Status != StatusRunning {
				t.Fatalf("a refused result must leave the task running, status = %s", got.Status)
			}
			// The agent fixes the file and completes.
			if _, err := s.Complete(task.ID, token, []byte(validRecordingResult)); err != nil {
				t.Fatalf("the corrected result is refused: %v", err)
			}
		})
	}
}

func TestRecordingResultSchemaRefusals(t *testing.T) {
	for name, edit := range map[string]func(m map[string]any){
		"no summary":   func(m map[string]any) { delete(m, "summary") },
		"no steps":     func(m map[string]any) { m["action"].(map[string]any)["steps"] = []any{} },
		"selectors []": func(m map[string]any) { m["selectors"] = []any{} },
	} {
		s, l := newService(t)
		task, token := startRecordingTask(t, s, l)
		if _, err := s.Complete(task.ID, token, mutate(t, edit)); code(err) != CodeSchemaInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRecordingContextIsRedactedByValue(t *testing.T) {
	s, l := newService(t)
	s.Redact = RedactValues([]string{"hunter2-vault-secret"})
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "draft.json"), []byte(`{"goal":"log in with hunter2-vault-secret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(context.Background(), SubmitInput{Kind: KindRecordingAnalysis, Goal: "g",
		Context: []ContextSource{{SourceDocument, src}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(l.cwd, filepath.FromSlash(ContextDir), task.ID, "document", filepath.Base(src), "draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hunter2-vault-secret") || !strings.Contains(string(b), "***") {
		t.Fatalf("context = %s", b)
	}
	if !strings.Contains(task.Prompt, "Context folder") || !strings.Contains(task.Prompt, "recording") {
		t.Fatalf("prompt = %s", task.Prompt)
	}
}
