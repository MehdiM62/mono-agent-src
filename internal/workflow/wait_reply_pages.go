package workflow

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// replyPage is the one template behind every reply page: a heading, the
// node's message, then either a result line, approval buttons, or a form.
var replyPage = template.Must(template.New("reply").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;margin:0;background:#f6f6f4;color:#1d1d1b}
main{max-width:34rem;margin:3rem auto;padding:1.5rem;background:#fff;border-radius:12px;box-shadow:0 1px 3px #0002}
h1{font-size:1.3rem;margin:0 0 .75rem}p.msg{white-space:pre-wrap}
label{display:block;margin:.9rem 0 .3rem;font-weight:600}
input[type=text],input[type=email],input[type=number],input[type=password],input[type=date],textarea,select{width:100%;box-sizing:border-box;padding:.5rem;font:inherit;border:1px solid #bbb;border-radius:6px}
textarea{min-height:7rem}.opt{display:block;font-weight:400;margin:.2rem 0}.opt input{margin-right:.4rem}
button{font:inherit;padding:.6rem 1.2rem;border:0;border-radius:6px;background:#1d1d1b;color:#fff;cursor:pointer;margin:1rem .5rem 0 0}
button.no{background:#fff;color:#1d1d1b;border:1px solid #1d1d1b}.err{color:#b00020}
@media (prefers-color-scheme:dark){body{background:#151514;color:#eee}main{background:#222220}button{background:#eee;color:#151514}button.no{background:#222220;color:#eee;border-color:#eee}input,textarea,select{background:#151514;color:#eee}}
</style></head><body><main>
<h1>{{.Title}}</h1>
{{with .Message}}<p class="msg">{{.}}</p>{{end}}
{{with .Done}}<p>{{.}}</p>{{end}}
{{if .Buttons}}<div>{{range .Buttons}}<form method="post" action="{{.Action}}" style="display:inline"><button{{if .No}} class="no"{{end}}>{{.Label}}</button></form>{{end}}</div>{{end}}
{{if .Fields}}<form method="post">
{{with .Problem}}<p class="err">{{.}}</p>{{end}}
{{range .Fields}}<label for="{{.ID}}">{{.Label}}{{if .Required}} *{{end}}</label>
{{if eq .Type "textarea"}}<textarea id="{{.ID}}" name="{{.ID}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>{{.Value}}</textarea>
{{else if eq .Type "dropdown"}}<select id="{{.ID}}" name="{{.ID}}"{{if .Multiple}} multiple{{end}}{{if .Required}} required{{end}}>{{if not .Multiple}}<option value=""></option>{{end}}{{range .Choices}}<option{{if .Checked}} selected{{end}}>{{.Value}}</option>{{end}}</select>
{{else if or (eq .Type "radio") (eq .Type "checkbox")}}{{$f := .}}{{range .Choices}}<label class="opt"><input type="{{$f.Type}}" name="{{$f.ID}}" value="{{.Value}}"{{if .Checked}} checked{{end}}>{{.Value}}</label>{{end}}
{{else}}<input id="{{.ID}}" name="{{.ID}}" type="{{.Type}}" value="{{.Value}}" placeholder="{{.Placeholder}}"{{if eq .Type "number"}} step="any"{{end}}{{if .Required}} required{{end}}>
{{end}}{{end}}
<button>{{.Submit}}</button></form>{{end}}
</main></body></html>
`))

type replyPageData struct {
	Title, Message, Done, Problem, Submit string
	Buttons                               []replyButton
	Fields                                []replyPageField
}

type replyButton struct {
	Action, Label string
	No            bool
}

type replyPageField struct {
	ID, Label, Type, Placeholder, Value string
	Required, Multiple                  bool
	Choices                             []replyChoice
}

type replyChoice struct {
	Value   string
	Checked bool
}

func writeReplyPage(w http.ResponseWriter, status int, d replyPageData) {
	if d.Title == "" {
		d.Title = "Your answer is requested"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = replyPage.Execute(w, d)
}

func renderReplyDone(w http.ResponseWriter, status int, spec WaitReplySpec, done string) {
	writeReplyPage(w, status, replyPageData{Title: spec.Title, Done: done})
}

// renderApprovalPage shows the approval buttons: both on the reply page,
// or the one a link named (action) as a confirmation.
func renderApprovalPage(w http.ResponseWriter, spec WaitReplySpec, token, action string) {
	approve, decline := spec.ApproveLabel, spec.DisapproveLabel
	if approve == "" {
		approve = "Approve"
	}
	if decline == "" {
		decline = "Decline"
	}
	var buttons []replyButton
	switch action {
	case "approve":
		buttons = []replyButton{{Action: "", Label: approve}}
	case "decline":
		buttons = []replyButton{{Action: "", Label: decline, No: true}}
	default:
		buttons = []replyButton{{Action: token + "/approve", Label: approve}}
		if spec.ApprovalType == "double" {
			buttons = append(buttons, replyButton{Action: token + "/decline", Label: decline, No: true})
		}
	}
	writeReplyPage(w, http.StatusOK, replyPageData{Title: spec.Title, Message: spec.Message, Buttons: buttons})
}

// replyFormFields are the fields a form reply asks.
func replyFormFields(row *replyRow) []WaitReplyField {
	if row.kind == WaitReplyFreeText {
		return []WaitReplyField{{Label: "Response", Name: "text", Type: "textarea", Required: true}}
	}
	return row.spec.FormFields
}

// renderFormPage shows the form, refilled from posted when re-asked.
func renderFormPage(w http.ResponseWriter, status int, spec WaitReplySpec, fields []WaitReplyField, posted url.Values, problem string) {
	d := replyPageData{Title: spec.Title, Message: spec.Message, Problem: problem, Submit: spec.ButtonLabel}
	if d.Submit == "" {
		d.Submit = "Submit"
	}
	for i, f := range fields {
		id := "f" + strconv.Itoa(i)
		typ := f.Type
		switch typ {
		case "text", "textarea", "number", "email", "password", "date", "dropdown", "radio", "checkbox":
		default:
			typ = "text"
		}
		pf := replyPageField{ID: id, Label: f.Label, Type: typ, Placeholder: f.Placeholder,
			Value: posted.Get(id), Required: f.Required, Multiple: f.Multiple}
		for _, o := range f.Options {
			pf.Choices = append(pf.Choices, replyChoice{Value: o, Checked: slices.Contains(posted[id], o)})
		}
		d.Fields = append(d.Fields, pf)
	}
	writeReplyPage(w, status, d)
}

// readReplyForm turns a posted form into the answer, or names the problem.
// Numbers become numbers; checkboxes and multi-select dropdowns a list.
func readReplyForm(fields []WaitReplyField, posted url.Values) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(fields))
	for i, f := range fields {
		vals := posted["f"+strconv.Itoa(i)]
		var nonEmpty []string
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				nonEmpty = append(nonEmpty, v)
			}
		}
		if f.Required && len(nonEmpty) == 0 {
			return nil, fmt.Sprintf("%q is required.", f.Label)
		}
		choice := f.Type == "dropdown" || f.Type == "radio" || f.Type == "checkbox"
		for _, v := range nonEmpty {
			if choice && !slices.Contains(f.Options, v) {
				return nil, fmt.Sprintf("%q is not an option of %q.", v, f.Label)
			}
		}
		switch {
		case f.Type == "checkbox" || (f.Type == "dropdown" && f.Multiple):
			list := make([]interface{}, len(nonEmpty))
			for j, v := range nonEmpty {
				list[j] = v
			}
			out[f.Key()] = list
		case len(nonEmpty) == 0:
			out[f.Key()] = ""
		case f.Type == "number":
			n, err := strconv.ParseFloat(strings.TrimSpace(nonEmpty[0]), 64)
			if err != nil {
				return nil, fmt.Sprintf("%q must be a number.", f.Label)
			}
			out[f.Key()] = n
		default:
			out[f.Key()] = nonEmpty[0]
		}
	}
	return out, ""
}

// answerSummary says what was answered, for the result page.
func answerSummary(row *replyRow) string {
	if row.kind == WaitReplyApproval {
		approved, _ := row.answer["approved"].(bool)
		if approved {
			return "The answer recorded is: approved."
		}
		return "The answer recorded is: declined."
	}
	return "The answer has been recorded."
}
