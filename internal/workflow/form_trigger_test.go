package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func formWorkflow(extra map[string]interface{}) *Workflow {
	cfg := map[string]interface{}{
		"path": "contact", "formTitle": "Contact <us>", "formDescription": "We answer fast.",
		"formFields": []interface{}{
			map[string]interface{}{"fieldLabel": "Name", "requiredField": true},
			map[string]interface{}{"fieldLabel": "Email", "fieldType": "email", "requiredField": true},
			map[string]interface{}{"fieldLabel": "Age", "fieldType": "number"},
			map[string]interface{}{"fieldLabel": "Topic", "fieldType": "dropdown",
				"fieldOptions": map[string]interface{}{"values": []interface{}{map[string]interface{}{"option": "Sales"}, map[string]interface{}{"option": "Support"}}}},
			map[string]interface{}{"fieldLabel": "When", "fieldType": "date"},
			map[string]interface{}{"fieldLabel": "Notes", "fieldType": "textarea"},
			map[string]interface{}{"fieldLabel": "Newsletter", "fieldType": "checkbox"},
			map[string]interface{}{"fieldLabel": "Channels", "fieldType": "checkbox", "options": []interface{}{"Mail", "Phone"}},
		},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return &Workflow{ID: "wf-form", Nodes: []WorkflowNode{{ID: "f", Type: TriggerNodeTypeForm, Config: cfg}}}
}

func formTM(t *testing.T, wf *Workflow) (*TriggerManager, *[]Item) {
	t.Helper()
	var got []Item
	tm := NewTriggerManager(nil, NewWebhookServer(":0", zerolog.Nop()), &fakeScheduler{}, func(_, _ string, items []Item) {
		got = append(got, items...)
	}, zerolog.Nop())
	if err := tm.ActivateWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("activate: %v", err)
	}
	return tm, &got
}

func submit(tm *TriggerManager, vals url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/form/contact", strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tm.webhookServer.ServeHTTP(rec, req)
	return rec
}

func TestFormTriggerRendersForm(t *testing.T) {
	tm, _ := formTM(t, formWorkflow(nil))
	rec := httptest.NewRecorder()
	tm.webhookServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/form/contact", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"Contact &lt;us&gt;", `<label for="field-0">Name`, `type="email"`, `aria-required="true"`,
		`<option>Support</option>`, `type="date"`, "<textarea", `<legend>Channels`, `<button type="submit">Submit</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form page lacks %q", want)
		}
	}
	rec = httptest.NewRecorder()
	tm.webhookServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/form/other", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown form: %d", rec.Code)
	}
}

func TestFormTriggerSubmitStartsRun(t *testing.T) {
	tm, got := formTM(t, formWorkflow(nil))
	rec := submit(tm, url.Values{
		"field-0": {"Ada"}, "field-1": {"ada@example.com"}, "field-2": {"36"}, "field-3": {"Support"},
		"field-4": {"2026-10-14"}, "field-6": {"on"}, "field-7": {"Mail", "Fax"},
	})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), defaultFormResponse) {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	if len(*got) != 1 {
		t.Fatalf("runs = %d", len(*got))
	}
	j := (*got)[0].JSON
	if j["Name"] != "Ada" || j["Email"] != "ada@example.com" || j["Age"] != 36.0 || j["Topic"] != "Support" ||
		j["When"] != "2026-10-14" || j["Notes"] != "" || j["Newsletter"] != true || j["submittedAt"] == nil || j["formMode"] != "production" {
		t.Fatalf("item = %v", j)
	}
	if ch, _ := j["Channels"].([]string); len(ch) != 1 || ch[0] != "Mail" {
		t.Fatalf("Channels = %#v (an unknown option must be dropped)", j["Channels"])
	}
}

func TestFormTriggerValidation(t *testing.T) {
	tm, got := formTM(t, formWorkflow(nil))
	rec := submit(tm, url.Values{"field-1": {"not-an-email"}, "field-2": {"x"}, "field-3": {"Other"}, "field-4": {"14/10/2026"}})
	body := rec.Body.String()
	if rec.Code != 400 || len(*got) != 0 {
		t.Fatalf("invalid submit: %d runs %d", rec.Code, len(*got))
	}
	for _, want := range []string{
		`role="alert"`, "This field is required.", "Enter an email address", "Enter a number.",
		"Choose one of the options.", "Enter a date as YYYY-MM-DD.", `aria-invalid="true"`, `value="not-an-email"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("error page lacks %q", want)
		}
	}
}

func TestFormTriggerRedirect(t *testing.T) {
	tm, got := formTM(t, formWorkflow(map[string]interface{}{"respondWith": "redirect", "redirectUrl": "https://example.com/thanks"}))
	rec := submit(tm, url.Values{"field-0": {"A"}, "field-1": {"a@b.co"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://example.com/thanks" || len(*got) != 1 {
		t.Fatalf("redirect: %d %q runs %d", rec.Code, rec.Header().Get("Location"), len(*got))
	}
}

func TestFormTriggerConfigErrors(t *testing.T) {
	for name, cfg := range map[string]map[string]interface{}{
		"no path":             {"path": ""},
		"no fields":           {"formFields": []interface{}{}},
		"bad type":            {"formFields": `[{"fieldLabel":"A","fieldType":"color"}]`},
		"dropdown no opts":    {"formFields": `[{"fieldLabel":"A","fieldType":"dropdown"}]`},
		"duplicate labels":    {"formFields": `[{"fieldLabel":"A"},{"fieldLabel":"A"}]`},
		"redirect no url":     {"respondWith": "redirect"},
		"redirect javascript": {"respondWith": "redirect", "redirectUrl": "javascript:alert(1)"},
	} {
		tm := NewTriggerManager(nil, NewWebhookServer(":0", zerolog.Nop()), &fakeScheduler{}, func(string, string, []Item) {}, zerolog.Nop())
		if err := tm.ActivateWorkflow(context.Background(), formWorkflow(cfg)); err == nil {
			t.Errorf("%s: activation succeeded", name)
		}
	}
}

func TestFormTriggerDeactivate(t *testing.T) {
	tm, _ := formTM(t, formWorkflow(map[string]interface{}{"formFields": `[{"fieldLabel":"Name"}]`}))
	tm.DeactivateWorkflow("wf-form")
	rec := httptest.NewRecorder()
	tm.webhookServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/form/contact", nil))
	if rec.Code != 404 {
		t.Fatalf("after deactivation: %d", rec.Code)
	}
}

func TestFormTriggerThroughRunner(t *testing.T) {
	tm, _ := formTM(t, formWorkflow(map[string]interface{}{"formFields": `[{"fieldLabel":"Name"}]`}))
	var started []Item
	tm.webhookServer.SetRunner(func(wf, node string, items []Item, _ func(string)) (string, error) {
		if wf != "wf-form" || node != "f" {
			t.Errorf("run(%q, %q)", wf, node)
		}
		started = items
		return "exec-1", nil
	}, nil)
	if rec := submit(tm, url.Values{"field-0": {"Ada"}}); rec.Code != 200 || len(started) != 1 || started[0].JSON["Name"] != "Ada" {
		t.Fatalf("runner: %d %v", rec.Code, started)
	}
	tm.webhookServer.SetRunner(func(string, string, []Item, func(string)) (string, error) {
		return "", ErrWebhookWorkflowInactive
	}, nil)
	if rec := submit(tm, url.Values{"field-0": {"Ada"}}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "could not be recorded") {
		t.Fatalf("inactive: %d %s", rec.Code, rec.Body.String())
	}
}
