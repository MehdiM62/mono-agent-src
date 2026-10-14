package workflow

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// formRoute is one active trigger.form node.
type formRoute struct {
	WorkflowID string
	NodeID     string
	Path       string
	Spec       *formSpec
	TriggerFn  func(items []Item) // when the server has no runner
}

// formRoutes are the server's forms, by path.
type formRoutes struct {
	mu sync.RWMutex
	m  map[string]*formRoute
}

// RegisterForm serves route at /form/<path>. The path must be unused.
func (s *WebhookServer) RegisterForm(route *formRoute) error {
	if route.Path == "" {
		return fmt.Errorf("form: path must not be empty")
	}
	s.forms.mu.Lock()
	defer s.forms.mu.Unlock()
	if s.forms.m == nil {
		s.forms.m = make(map[string]*formRoute)
	}
	if _, ok := s.forms.m[route.Path]; ok {
		return fmt.Errorf("form: path %q is already registered", route.Path)
	}
	s.forms.m[route.Path] = route
	return nil
}

// DeregisterForm stops serving the form at path.
func (s *WebhookServer) DeregisterForm(path string) {
	s.forms.mu.Lock()
	defer s.forms.mu.Unlock()
	delete(s.forms.m, path)
}

// activateForm serves a trigger.form node's form.
func (tm *TriggerManager) activateForm(workflowID string, node *WorkflowNode) error {
	if node.Config == nil {
		if err := node.ParseConfig(); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	path, _ := node.Config["path"].(string)
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return fmt.Errorf("trigger.form: missing or invalid \"path\" field in config")
	}
	spec, err := parseFormSpec(node.Config)
	if err != nil {
		return err
	}
	wfID, nID := workflowID, node.ID
	route := &formRoute{WorkflowID: wfID, NodeID: nID, Path: path, Spec: spec,
		TriggerFn: func(items []Item) { tm.triggerFn(wfID, nID, items) }}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, exists := tm.active[workflowID][node.ID]; exists {
		return nil
	}
	if err := tm.webhookServer.RegisterForm(route); err != nil {
		return err
	}
	if tm.active[workflowID] == nil {
		tm.active[workflowID] = make(map[string]*triggerEntry)
	}
	tm.active[workflowID][node.ID] = &triggerEntry{kind: "form", webhookPath: path}
	tm.logger.Info().Str("workflow_id", workflowID).Str("node_id", node.ID).Str("path", path).Msg("form trigger activated")
	return nil
}

// serveForm answers /form/<path>: GET shows the form, POST validates it and
// starts a run with the answers.
func (s *WebhookServer) serveForm(w http.ResponseWriter, r *http.Request, path string) {
	s.forms.mu.RLock()
	route := s.forms.m[strings.Trim(path, "/")]
	s.forms.mu.RUnlock()
	if route == nil {
		http.Error(w, "form not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		renderForm(w, http.StatusOK, route.Spec, nil, nil)
	case http.MethodPost:
		s.submitForm(w, r, route)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *WebhookServer) submitForm(w http.ResponseWriter, r *http.Request, route *formRoute) {
	r.Body = http.MaxBytesReader(w, r.Body, defaultWebhookMaxBody)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(defaultWebhookMaxBody)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		renderFormMessage(w, http.StatusBadRequest, route.Spec.Title, "The form could not be read. Please try again.")
		return
	}
	answers, errs := route.Spec.answers(r.PostForm)
	if len(errs) > 0 {
		renderForm(w, http.StatusBadRequest, route.Spec, r.PostForm, errs)
		return
	}
	answers["submittedAt"] = time.Now().UTC().Format(time.RFC3339)
	answers["formMode"] = "production"
	trace, status, msg := s.admitTrace(r.Context(), route.WorkflowID, "")
	if status != 0 {
		renderFormMessage(w, status, route.Spec.Title, msg)
		return
	}
	if trace != nil {
		answers[WebhookTraceKey] = trace
	}
	items := []Item{{JSON: answers}}

	s.mu.RLock()
	run := s.runner
	s.mu.RUnlock()
	if run == nil {
		route.TriggerFn(items)
	} else if _, err := run(route.WorkflowID, route.NodeID, items, nil); err != nil {
		code, msg := webhookStartStatus(err)
		if code >= 500 && code != http.StatusServiceUnavailable {
			s.logger.Error().Err(err).Str("workflow_id", route.WorkflowID).Msg("form: starting the run")
		}
		renderFormMessage(w, code, route.Spec.Title, "Your response could not be recorded ("+msg+"). Please try again later.")
		return
	}
	if route.Spec.RespondWith == "redirect" {
		http.Redirect(w, r, route.Spec.RedirectURL, http.StatusSeeOther)
		return
	}
	renderFormMessage(w, http.StatusOK, route.Spec.Title, route.Spec.ResponseText)
}

// formView is what the page template renders.
type formView struct {
	Title       string
	Description string
	Button      string
	Fields      []formFieldView
	Message     string
	HasErrors   bool
}

type formFieldView struct {
	formField
	Name    string
	Value   string
	Checked map[string]bool
	Error   string
}

func renderForm(w http.ResponseWriter, code int, spec *formSpec, values map[string][]string, errs map[int]string) {
	v := formView{Title: spec.Title, Description: spec.Description, Button: spec.ButtonLabel, HasErrors: len(errs) > 0}
	for i, f := range spec.Fields {
		fv := formFieldView{formField: f, Name: formInputName(i), Error: errs[i], Checked: map[string]bool{}}
		for _, val := range values[fv.Name] {
			fv.Checked[val] = true
			if fv.Value == "" {
				fv.Value = val
			}
		}
		v.Fields = append(v.Fields, fv)
	}
	writeFormPage(w, code, v)
}

func renderFormMessage(w http.ResponseWriter, code int, title, msg string) {
	writeFormPage(w, code, formView{Title: title, Message: msg})
}

func writeFormPage(w http.ResponseWriter, code int, v formView) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = formPage.Execute(w, v)
}

var formPage = template.Must(template.New("form").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Title}}{{.Title}}{{else}}Form{{end}}</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;margin:0;background:#f6f6f4;color:#1d1d1b}
main{max-width:36rem;margin:2rem auto;padding:1.5rem;background:#fff;border-radius:8px}
label,legend{display:block;font-weight:600;margin-top:1.25rem}
input:not([type=checkbox]),select,textarea{display:block;width:100%;box-sizing:border-box;margin-top:.35rem;padding:.55rem;font:inherit;border:1px solid #8a8a85;border-radius:4px}
fieldset{border:0;padding:0;margin:0}fieldset label{font-weight:400;margin-top:.35rem}
[aria-invalid=true]{border-color:#b3261e}.error{color:#b3261e;margin:.25rem 0 0}
button{margin-top:1.5rem;padding:.65rem 1.4rem;font:inherit;border:0;border-radius:4px;background:#1d4ed8;color:#fff;cursor:pointer}
:focus-visible{outline:3px solid #f59e0b;outline-offset:2px}
@media (prefers-color-scheme:dark){body{background:#151514;color:#eee}main{background:#232321}input:not([type=checkbox]),select,textarea{background:#1b1b1a;color:#eee}.error{color:#ff8a80}[aria-invalid=true]{border-color:#ff8a80}}
</style></head><body><main>
{{if .Title}}<h1>{{.Title}}</h1>{{end}}
{{if .Message}}<p role="status">{{.Message}}</p>{{else}}
{{if .Description}}<p>{{.Description}}</p>{{end}}
{{if .HasErrors}}<p class="error" role="alert">Please correct the fields marked below.</p>{{end}}
<form method="post" novalidate>
{{range .Fields}}{{$f := .}}
{{if and (eq .Type "checkbox") .Options}}
<fieldset{{if .Error}} aria-describedby="{{.Name}}-error"{{end}}><legend>{{.Label}}{{if .Required}} <span aria-hidden="true">*</span>{{end}}</legend>
{{range $i, $o := .Options}}<label><input type="checkbox" name="{{$f.Name}}" value="{{$o}}"{{if index $f.Checked $o}} checked{{end}}> {{$o}}</label>{{end}}
</fieldset>
{{else if eq .Type "checkbox"}}
<label><input type="checkbox" id="{{.Name}}" name="{{.Name}}" value="on"{{if .Value}} checked{{end}}{{if .Required}} required aria-required="true"{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.Name}}-error"{{end}}> {{.Label}}</label>
{{else}}
<label for="{{.Name}}">{{.Label}}{{if .Required}} <span aria-hidden="true">*</span>{{end}}</label>
{{if eq .Type "textarea"}}<textarea id="{{.Name}}" name="{{.Name}}" rows="4"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required aria-required="true"{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.Name}}-error"{{end}}>{{.Value}}</textarea>
{{else if eq .Type "dropdown"}}<select id="{{.Name}}" name="{{.Name}}"{{if .Required}} required aria-required="true"{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.Name}}-error"{{end}}><option value="">Choose…</option>{{range .Options}}<option{{if eq . $f.Value}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else}}<input id="{{.Name}}" name="{{.Name}}" type="{{.Type}}" value="{{.Value}}"{{if eq .Type "number"}} step="any"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required aria-required="true"{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.Name}}-error"{{end}}>
{{end}}{{end}}
{{if .Error}}<p class="error" id="{{.Name}}-error">{{.Error}}</p>{{end}}
{{end}}
<button type="submit">{{.Button}}</button>
</form>{{end}}
</main></body></html>
`))
