package apipkg

// The catalog test harness: every package in catalog/ is validated, and
// every operation is run against an httptest server with the sample params
// from catalog/testdata/<id>.json. See FORMAT.md "Testing a package".

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// opCase is one entry of catalog/testdata/<id>.json.
type opCase struct {
	Params     map[string]any `json:"params"`
	Credential map[string]any `json:"credential,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	// Response is the JSON the mock server answers the first request with
	// (default {}); later requests get {} so pagination stops.
	Response       any `json:"response,omitempty"`
	ResponseStatus int `json:"response_status,omitempty"`
	Expect         struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   map[string]any    `json:"query"`
		Body    any               `json:"body"`
		Headers map[string]string `json:"headers"`
		Items   *int              `json:"items"`
	} `json:"expect"`
}

// captured is one request the mock server saw.
type captured struct {
	method, path string
	query        url.Values
	header       http.Header
	body         []byte
}

// mockServer answers every request and records it. All package traffic is
// rerouted to it by swapping httpClient's transport.
type mockServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	reqs  []captured
	first []byte
	code  int
}

func newMockServer(t *testing.T, response any, status int) *mockServer {
	m := &mockServer{code: status}
	if m.code == 0 {
		m.code = 200
	}
	if response != nil {
		m.first, _ = json.Marshal(response)
	}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.reqs = append(m.reqs, captured{r.Method, r.URL.EscapedPath(), r.URL.Query(), r.Header.Clone(), b})
		n := len(m.reqs)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(m.code)
			if m.first != nil {
				w.Write(m.first)
				return
			}
		}
		w.Write([]byte("{}"))
	}))
	t.Cleanup(m.srv.Close)
	target, _ := url.Parse(m.srv.URL)
	old := httpClient
	httpClient = &http.Client{Transport: rewriteTransport{target: target}}
	t.Cleanup(func() { httpClient = old })
	return m
}

// rewriteTransport sends every request to target, keeping path and query.
type rewriteTransport struct{ target *url.URL }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = rt.target.Scheme, rt.target.Host, rt.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func (m *mockServer) requests() []captured {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]captured(nil), m.reqs...)
}

// fakeCredential fills every credential field with "test-<key>".
func fakeCredential(p *Package) map[string]any {
	cred := map[string]any{"access_token": "test-access_token"}
	for _, c := range p.Credentials {
		for _, f := range c.Fields {
			cred[f.Key] = "test-" + f.Key
		}
	}
	return cred
}

// runPackageOp runs one op of p with params and returns the node's items.
func runPackageOp(p *Package, params, cred, input map[string]any) ([]workflow.Item, error) {
	cfg := map[string]any{}
	for k, v := range params {
		cfg[k] = v
	}
	if cred != nil {
		cfg["credential"] = cred
	}
	in := workflow.NodeInput{}
	if input != nil {
		in.Items = []workflow.Item{{JSON: input}}
	}
	out, err := run(context.Background(), p, in, cfg, true)
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

// checkCase runs one testdata case and reports every mismatch.
func checkCase(t *testing.T, p *Package, op *Operation, c opCase) {
	t.Helper()
	where := fmt.Sprintf("%s · %s", p.ID, op.Key())
	params := map[string]any{"resource": op.Resource, "operation": op.Operation}
	for k, v := range c.Params {
		params[k] = v
	}
	cred := c.Credential
	if cred == nil {
		cred = fakeCredential(p)
	}
	srv := newMockServer(t, c.Response, c.ResponseStatus)
	items, err := runPackageOp(p, params, cred, c.Input)
	if err != nil {
		t.Fatalf("%s: the op failed: %v\n  (fix the package or the case's params)", where, err)
	}
	reqs := srv.requests()
	if len(reqs) == 0 {
		t.Fatalf("%s: no request reached the server", where)
	}
	got := reqs[0]
	e := c.Expect
	if e.Method != "" && got.method != e.Method {
		t.Errorf("%s: method: got %s, want %s", where, got.method, e.Method)
	}
	if got.path != e.Path {
		t.Errorf("%s: path: got %q, want %q", where, got.path, e.Path)
	}
	credDef := p.selectCredential(cred)
	checkQuery(t, where, got.query, e.Query, credDef)
	checkBody(t, where, got, e.Body)
	for k, want := range e.Headers {
		if g := got.header.Get(k); g != want {
			t.Errorf("%s: header %s: got %q, want %q", where, k, g, want)
		}
	}
	checkAuth(t, where, got, credDef, cred)
	if e.Items != nil && len(items) != *e.Items {
		t.Errorf("%s: items: got %d, want %d", where, len(items), *e.Items)
	}
}

func checkQuery(t *testing.T, where string, got url.Values, want map[string]any, c *Credential) {
	t.Helper()
	skip := map[string]bool{}
	if c != nil {
		for k := range c.Inject.Query {
			skip[k] = true
		}
	}
	gotM := map[string]any{}
	for k, vs := range got {
		if skip[k] {
			continue
		}
		if len(vs) == 1 {
			gotM[k] = vs[0]
		} else {
			arr := make([]any, len(vs))
			for i, v := range vs {
				arr[i] = v
			}
			gotM[k] = arr
		}
	}
	wantM := map[string]any{}
	for k, v := range want {
		if arr, ok := v.([]any); ok {
			s := make([]any, len(arr))
			for i, el := range arr {
				s[i] = stringify(el)
			}
			wantM[k] = s
			continue
		}
		wantM[k] = stringify(v)
	}
	if !reflect.DeepEqual(gotM, wantM) {
		t.Errorf("%s: query: got %s, want %s (expect.query must list every param sent, auth query params excepted)",
			where, jsonText(gotM), jsonText(wantM))
	}
}

func checkBody(t *testing.T, where string, got captured, want any) {
	t.Helper()
	ct, params, _ := mime.ParseMediaType(got.header.Get("Content-Type"))
	if want == nil {
		if len(got.body) > 0 && ct != "application/json" {
			t.Errorf("%s: a %s body was sent but the case has no expect.body", where, ct)
		}
		if ct == "application/json" && len(got.body) > 0 && string(got.body) != "{}" && string(got.body) != "null" {
			t.Errorf("%s: body: got %s, but the case has no expect.body", where, got.body)
		}
		return
	}
	var gotV any
	switch ct {
	case "application/x-www-form-urlencoded":
		q, _ := url.ParseQuery(string(got.body))
		m := map[string]any{}
		for k := range q {
			m[k] = q.Get(k)
		}
		gotV = m
	case "multipart/form-data":
		m := map[string]any{}
		r := multipart.NewReader(strings.NewReader(string(got.body)), params["boundary"])
		for {
			part, err := r.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			if part.FileName() != "" {
				m[part.FormName()] = "file:" + part.FileName() + ":" + string(b)
			} else {
				m[part.FormName()] = string(b)
			}
		}
		gotV = m
	case "application/json":
		_ = json.Unmarshal(got.body, &gotV)
	default:
		gotV = string(got.body)
	}
	if !reflect.DeepEqual(normalize(gotV), normalize(want)) {
		t.Errorf("%s: body: got %s, want %s", where, jsonText(gotV), jsonText(want))
	}
}

// normalize round-trips through JSON so numbers compare as float64.
func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func checkAuth(t *testing.T, where string, got captured, c *Credential, cred map[string]any) {
	t.Helper()
	if c == nil {
		return
	}
	sc := Scope{Cred: cred}
	for k, tv := range c.Inject.Headers {
		want, _ := renderText(tv, sc)
		if g := got.header.Get(k); g != want {
			t.Errorf("%s: auth header %s: got %q, want %q (from inject.headers)", where, k, g, want)
		}
	}
	for k, tv := range c.Inject.Query {
		want, _ := renderText(tv, sc)
		if g := got.query.Get(k); g != want {
			t.Errorf("%s: auth query %s: got %q, want %q (from inject.query)", where, k, g, want)
		}
	}
	if c.Inject.Basic != nil && !strings.HasPrefix(got.header.Get("Authorization"), "Basic ") {
		t.Errorf("%s: inject.basic is set but no Basic Authorization header was sent", where)
	}
	if c.Inject.AWSSigV4 != nil && !strings.HasPrefix(got.header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		t.Errorf("%s: inject.aws_sigv4 is set but the request is not SigV4-signed", where)
	}
}

// checkRequiredFields drops each required field (one at a time) and wants
// an error naming it, with no request sent.
func checkRequiredFields(t *testing.T, p *Package, op *Operation, c opCase) {
	t.Helper()
	for _, f := range op.Fields {
		if !f.Required || f.Default != nil {
			continue
		}
		params := map[string]any{"resource": op.Resource, "operation": op.Operation}
		for k, v := range c.Params {
			if k != f.Key {
				params[k] = v
			}
		}
		srv := newMockServer(t, nil, 0)
		_, err := runPackageOp(p, params, fakeCredential(p), c.Input)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", f.Key)) {
			t.Errorf("%s · %s: without required field %q the op should fail naming it; got err=%v", p.ID, op.Key(), f.Key, err)
		}
		if n := len(srv.requests()); n > 0 {
			t.Errorf("%s · %s: without required field %q a request was still sent", p.ID, op.Key(), f.Key)
		}
	}
}

// loadCases reads catalog/testdata/<id>.json.
func loadCases(id string) (map[string]opCase, error) {
	b, err := os.ReadFile("catalog/testdata/" + id + ".json")
	if err != nil {
		return nil, err
	}
	var cases map[string]opCase
	if err := json.Unmarshal(b, &cases); err != nil {
		return nil, fmt.Errorf("catalog/testdata/%s.json: %w", id, err)
	}
	return cases, nil
}

func sortedCaseKeys(m map[string]opCase) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
