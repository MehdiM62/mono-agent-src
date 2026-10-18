package apipkg

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Reasons several ops share.
const (
	rawRead   = "a read: the JSON is its query or search filter"
	rawHive   = "the json-typed mapping holds the record's fields as TheHive's schema lists them; the request carries no message, and what TheHive announces is up to its own notifiers"
	rawIssue  = "json-typed lists of label and assignee names, each plucked, not raw JSON; an issue is a work item, open by the user's decision"
	rawPage   = "the JSON is the page body in Atlassian's document format, the content bodyStorage carries as markup; a page is no message"
	rawLines  = "the json-typed lines are mapped key by key ($map), not merged; creating one sends nothing (estimate, invoice and payment send are ops of their own, which hold)"
	rawMautic = "the JSON is the contact's fields; Mautic's contact API sends nothing (contact/sendEmail is an op of its own, which holds)"
	rawGitHub = "the JSON is the workflow's inputs only, which the account's own workflow reads as it was built"
)

// rawJSONReasons are the ops of a package that sends whose raw-JSON or
// json-typed params feed the request, yet send nothing through them, and
// why (TestRawJSONParamsAreDecided). Any other such op holds on them.
var rawJSONReasons = map[string]string{
	"click_up task/create":                "the JSON is the task's custom_fields, values of the list's own fields",
	"click_up task/setCustomField":        "the JSON is one custom field's value",
	"confluence page/create":              rawPage,
	"confluence page/append":              rawPage,
	"confluence page/update":              rawPage,
	"customer_io customer/upsert":         "the JSON is the customer's attributes, which PUT /customers/{id} stores and sends nothing with (a campaign an attribute starts is the account's own)",
	"customer_io campaign/getMetrics":     rawRead,
	"facebook_graph_api /GET":             rawRead,
	"facebook_graph_api /DELETE":          "a delete: the JSON is its query",
	"github issue/create":                 rawIssue,
	"github workflow/dispatch":            rawGitHub,
	"github workflow/dispatchAndWait":     rawGitHub,
	"gitlab issue/create":                 rawIssue,
	"magento2 customer/getAll":            rawRead,
	"magento2 order/getAll":               rawRead,
	"magento2 product/getAll":             rawRead,
	"mailchimp member/update":             "the JSON fills location, merge_fields and interests only, as the fields beside it do; a status change is what emails the member, and it holds",
	"mautic contact/create":               rawMautic,
	"mautic contact/update":               rawMautic,
	"misp attribute/search":               rawRead,
	"misp event/search":                   rawRead,
	"misp object/search":                  rawRead,
	"quick_books bill/create":             rawLines,
	"quick_books estimate/create":         rawLines,
	"quick_books invoice/create":          rawLines,
	"salesforce flow/invoke":              "the JSON is the flow's input variables only, which the account's own flow reads as it was built",
	"salesmate activity/getAll":           rawRead,
	"salesmate company/getAll":            rawRead,
	"salesmate deal/getAll":               rawRead,
	"the_hive_project alert/create":       rawHive,
	"the_hive_project alert/update":       rawHive,
	"the_hive_project case/create":        rawHive,
	"the_hive_project case/update":        rawHive,
	"the_hive_project log/create":         rawHive,
	"the_hive_project observable/create":  rawHive,
	"the_hive_project observable/update":  rawHive,
	"the_hive_project query/executeQuery": rawRead,
	"the_hive_project task/create":        rawHive,
	"the_hive_project task/update":        rawHive,
	"vero user/create":                    "the JSON is the user's data attributes; tracking a user sends nothing (events are what Vero's campaigns send on, and event/track holds)",
}

// In a package that sends, an op whose raw JSON feeds the request either
// holds on it or says why it can't send through it: raw JSON goes out as
// written, whatever the op's fields declare.
func TestRawJSONParamsAreDecided(t *testing.T) {
	pkgs := catalogPackages(t)
	if undecided := undecidedRawJSON(pkgs, rawJSONReasons); len(undecided) > 0 {
		t.Errorf("%d op(s) of a package that sends take raw JSON into the request but neither hold on it (an unconditional send, or an \"if\" "+
			"naming the param or its switch) nor have a reason in rawJSONReasons:\n  %s", len(undecided), strings.Join(undecided, "\n  "))
	}
	raw := map[string]bool{}
	for _, u := range undecidedRawJSON(pkgs, nil) {
		op, _, _ := strings.Cut(u, " (")
		raw[op] = true
	}
	for op := range rawJSONReasons {
		if !raw[op] {
			t.Errorf("rawJSONReasons names %s, which holds on its raw JSON or has none", op)
		}
	}
}

// The lint sees a planted op: raw JSON behind a switch in a package that
// sends, unheld, is caught; held on its switch, sent unconditionally, given
// a reason, unused by the request or in a package that never sends, it
// isn't; "!=" holds on nothing, "?" on its param.
func TestTheRawJSONLintCatchesAPlantedOp(t *testing.T) {
	fields := []workflow.NodeSchemaField{{Key: "jsonParameters", Type: "boolean"}, {Key: "bodyJson", Type: "json"}}
	switched := map[string]any{"$merge": "{{ p.jsonParameters | eq:'true' | then:(p.bodyJson | fromjson) }}"}
	none := &Sends{None: true}
	chat := &Package{ID: "acme_chat", Operations: []Operation{
		{Resource: "message", Operation: "post", Method: "POST", Path: "/m", Fields: fields, Body: switched, Sends: &Sends{Action: "post"}},
		{Resource: "contact", Operation: "upsert", Method: "PUT", Path: "/c", Fields: fields, Body: switched, Sends: none, SendsReason: "x"},
		{Resource: "stream", Operation: "create", Method: "POST", Path: "/s", Fields: fields, Body: switched,
			Sends: &Sends{Action: "post", If: Cond{"announce", "jsonParameters"}}},
		{Resource: "stream", Operation: "update", Method: "PATCH", Path: "/s/1", Fields: fields, Body: switched,
			Sends: &Sends{Action: "post", If: Cond{"announce"}}},
		{Resource: "stream", Operation: "rename", Method: "PATCH", Path: "/s/1", Fields: fields, Body: switched,
			Sends: &Sends{Action: "post", If: Cond{"jsonParameters!=true"}}},
		{Resource: "stream", Operation: "describe", Method: "PATCH", Path: "/s/1", Fields: fields, Body: switched,
			Sends: &Sends{Action: "post", If: Cond{"bodyJson?"}}},
		{Resource: "flow", Operation: "run", Method: "POST", Path: "/f", Fields: []workflow.NodeSchemaField{{Key: "inputs", Type: "text"}},
			Body: map[string]any{"inputs": "{{ p.inputs | fromjson }}"}, Sends: none, SendsReason: "x"},
		{Resource: "flow", Operation: "start", Method: "POST", Path: "/f", Fields: []workflow.NodeSchemaField{{Key: "inputs", Type: "text"}},
			Body: map[string]any{"inputs": "{{ p.inputs | fromjson }}"}, Sends: none, SendsReason: "x"},
		{Resource: "note", Operation: "create", Method: "POST", Path: "/n", Fields: fields, Body: map[string]any{"text": "x"}, Sends: none, SendsReason: "x"},
	}}
	crm := &Package{ID: "acme_crm", Operations: []Operation{
		{Resource: "contact", Operation: "upsert", Method: "PUT", Path: "/c", Fields: fields, Body: switched, Sends: none, SendsReason: "x"},
	}}
	got := undecidedRawJSON([]*Package{chat, crm}, map[string]string{"acme_chat flow/run": "the flow's own inputs"})
	want := []string{"acme_chat contact/upsert (bodyJson)", "acme_chat stream/update (bodyJson)", "acme_chat stream/rename (bodyJson)",
		"acme_chat flow/start (inputs)"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("undecided:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

var (
	// fromJSONParam is a param a template parses as raw JSON ("p.inputs |
	// fromjson", "(p.bodyJson | fromjson)").
	fromJSONParam = regexp.MustCompile(`p\.([\w.]+)\s*\)?\s*\|\s*fromjson\b`)
	templateText  = regexp.MustCompile(`\{\{.*?\}\}`)
	paramRef      = regexp.MustCompile(`p\.([\w.]+)`)
)

// undecidedRawJSON lists, for each package with a send, the ops whose
// request takes a raw-JSON param (a json-typed field, or one a template
// parses with fromjson) that the op doesn't hold on, as "pkg res/op
// (param)", unless reasons names the op.
func undecidedRawJSON(pkgs []*Package, reasons map[string]string) []string {
	var out []string
	for _, p := range pkgs {
		if !slices.ContainsFunc(p.Operations, func(o Operation) bool { return o.Sends != nil && !o.Sends.None }) {
			continue
		}
		for i := range p.Operations {
			o := &p.Operations[i]
			name := p.ID + " " + o.Resource + "/" + o.Operation
			if reasons[name] != "" {
				continue
			}
			req := requestText(o)
			for _, r := range rawJSONParams(o, req) {
				if !holdsOnRaw(o, r, req) {
					out = append(out, name+" ("+r+")")
				}
			}
		}
	}
	return out
}

// requestText is o's requests as JSON, without its fields, annotation and
// output.
func requestText(o *Operation) string {
	r := *o
	r.Title, r.Description, r.Fields, r.Sends, r.SendsReason = "", "", nil, nil, ""
	r.Output, r.Outputs, r.N8nParamMap, r.Subfields = nil, nil, nil, nil
	b, _ := json.Marshal(r)
	return string(b)
}

// rawJSONParams are o's raw-JSON params that req uses.
func rawJSONParams(o *Operation, req string) []string {
	var keys []string
	for _, f := range o.Fields {
		if f.Type == "json" {
			keys = append(keys, f.Key)
		}
	}
	for _, m := range fromJSONParam.FindAllStringSubmatch(req, -1) {
		keys = append(keys, m[1])
	}
	var out []string
	for _, k := range keys {
		if !slices.Contains(out, k) && regexp.MustCompile(`p\.`+regexp.QuoteMeta(k)+`\b`).MatchString(req) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// holdsOnRaw reports whether o waits whenever raw param r goes out: it
// always sends, or an "if" (set, =value, or present) names r or a param a
// template using r reads too (its jsonParameters switch).
func holdsOnRaw(o *Operation, r, req string) bool {
	s := o.Sends
	if s == nil || s.None {
		return false
	}
	if len(s.If) == 0 {
		return true
	}
	near := []string{r}
	for _, t := range templateText.FindAllString(req, -1) {
		if regexp.MustCompile(`p\.` + regexp.QuoteMeta(r) + `\b`).MatchString(t) {
			for _, m := range paramRef.FindAllStringSubmatch(t, -1) {
				near = append(near, m[1])
			}
		}
	}
	for _, c := range s.If {
		// "p!=v" and "p not in a|b" name no param here: neither holds on p;
		// "p?" does.
		name, _, _ := strings.Cut(c, "=")
		name = strings.TrimSuffix(name, "?")
		for _, k := range near {
			if k == name || strings.HasPrefix(k, name+".") {
				return true
			}
		}
	}
	return false
}
