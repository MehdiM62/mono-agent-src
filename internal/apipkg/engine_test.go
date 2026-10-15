package apipkg

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// mustParse parses a package from JSON for tests.
func mustParse(t *testing.T, js string) *Package {
	t.Helper()
	p, err := Parse([]byte(js), "test")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// pkgJSON builds a package with one credential (bearer) and the given ops.
func pkgJSON(ops string) string {
	return `{"$schema":"apipkg.v1","id":"demo","title":"Demo","base_url":"https://api.demo.test/v1",
	"credentials":[{"platform":"demo","methods":["bearer"],"fields":[{"key":"token","label":"Token","secret":true,"required":true}],
	  "inject":{"headers":{"Authorization":"Bearer {{ cred.token }}"}}}],
	"operations":[` + ops + `]}`
}

// pagedServer serves pages by request count via fn.
func pagedServer(t *testing.T, fn func(n int, r *http.Request, w http.ResponseWriter)) *[]*http.Request {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		w.Header().Set("Content-Type", "application/json")
		fn(len(seen), r, w)
	}))
	t.Cleanup(srv.Close)
	old := httpClient
	httpClient = &http.Client{Transport: rewriteTransport{target: mustURL(srv.URL)}}
	t.Cleanup(func() { httpClient = old })
	return &seen
}

var cred = map[string]any{"token": "T"}

func TestCursorPaginationReturnAllAndLimit(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"list","title":"List","method":"GET","path":"/items",
	  "fields":[{"key":"returnAll","label":"All","type":"boolean","default":false},{"key":"limit","label":"Limit","type":"number","default":50}],
	  "pagination":{"type":"cursor","cursor_param":"after","cursor_path":"meta.next","has_more_path":"meta.more","limit_param":"per","page_size":2,"results_path":"data"}}`))
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		next, more := fmt.Sprintf("c%d", n), n < 3
		fmt.Fprintf(w, `{"data":[{"n":%d},{"n":%d}],"meta":{"next":%q,"more":%v}}`, 2*n-1, 2*n, next, more)
	})
	items, err := runPackageOp(p, map[string]any{"operation": "list", "returnAll": true}, cred, nil)
	if err != nil || len(items) != 6 {
		t.Fatalf("returnAll: %d items, %v", len(items), err)
	}
	if q := (*seen)[1].URL.Query(); q.Get("after") != "c1" || q.Get("per") != "2" {
		t.Errorf("second page query: %v", q)
	}
	if (*seen)[0].URL.Query().Get("after") != "" {
		t.Error("first page must not send a cursor")
	}
	*seen = nil
	items, err = runPackageOp(p, map[string]any{"operation": "list", "limit": 3}, cred, nil)
	if err != nil || len(items) != 3 || len(*seen) != 2 {
		t.Fatalf("limit 3: %d items, %d requests, %v", len(items), len(*seen), err)
	}
}

func TestOffsetPageLinkNextURLPagination(t *testing.T) {
	cases := []struct {
		name, pg string
		serve    func(n int, r *http.Request, w http.ResponseWriter)
		check    func(t *testing.T, reqs []*http.Request)
		want     int
	}{
		{"offset", `{"type":"offset","offset_param":"skip","limit_param":"take","page_size":2}`,
			func(n int, r *http.Request, w http.ResponseWriter) {
				if n < 3 {
					fmt.Fprint(w, `[{"a":1},{"a":2}]`)
				} else {
					fmt.Fprint(w, `[{"a":3}]`)
				}
			},
			func(t *testing.T, reqs []*http.Request) {
				if reqs[2].URL.Query().Get("skip") != "4" || reqs[0].URL.Query().Get("skip") != "0" {
					t.Errorf("offsets: %v %v", reqs[0].URL.Query(), reqs[2].URL.Query())
				}
			}, 5},
		{"page", `{"type":"page","page_param":"p","limit_param":"size","page_size":2,"start_page":0}`,
			func(n int, r *http.Request, w http.ResponseWriter) {
				if n < 2 {
					fmt.Fprint(w, `[{"a":1},{"a":2}]`)
				} else {
					fmt.Fprint(w, `[]`)
				}
			},
			func(t *testing.T, reqs []*http.Request) {
				if reqs[0].URL.Query().Get("p") != "0" || reqs[1].URL.Query().Get("p") != "1" {
					t.Errorf("pages: %v", reqs[1].URL.Query())
				}
			}, 2},
		{"link_header", `{"type":"link_header"}`,
			func(n int, r *http.Request, w http.ResponseWriter) {
				if n < 3 {
					w.Header().Set("Link", fmt.Sprintf(`<https://api.demo.test/v1/items?page=%d>; rel="next", <https://x/last>; rel="last"`, n+1))
				}
				fmt.Fprint(w, `[{"a":1}]`)
			},
			func(t *testing.T, reqs []*http.Request) {
				if reqs[2].URL.Query().Get("page") != "3" || reqs[2].Header.Get("Authorization") != "Bearer T" {
					t.Errorf("link: %v %v", reqs[2].URL, reqs[2].Header)
				}
			}, 3},
		{"next_url", `{"type":"next_url","next_url_path":"paging.next","results_path":"items"}`,
			func(n int, r *http.Request, w http.ResponseWriter) {
				next := ""
				if n < 2 {
					next = "/v1/items?from=abc" // relative URLs resolve against the current one
				}
				fmt.Fprintf(w, `{"items":[{"a":%d}],"paging":{"next":%q}}`, n, next)
			},
			func(t *testing.T, reqs []*http.Request) {
				if reqs[1].URL.Query().Get("from") != "abc" || reqs[1].URL.Path != "/v1/items" {
					t.Errorf("next_url: %v", reqs[1].URL)
				}
			}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustParse(t, pkgJSON(`{"resource":"","operation":"list","title":"List","method":"GET","path":"/items","pagination":`+c.pg+`}`))
			seen := pagedServer(t, c.serve)
			items, err := runPackageOp(p, map[string]any{}, cred, nil)
			if err != nil || len(items) != c.want {
				t.Fatalf("%d items, %v", len(items), err)
			}
			c.check(t, *seen)
		})
	}
}

func TestPaginationMaxPagesTruncates(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"list","title":"List","method":"GET","path":"/items",
	  "pagination":{"type":"page","page_param":"page","max_pages":3}}`))
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) { fmt.Fprint(w, `[{"a":1}]`) })
	items, err := runPackageOp(p, map[string]any{}, cred, nil)
	if err != nil || len(items) != 3 || items[0].JSON["pagination_truncated"] != true {
		t.Fatalf("%v %v", items, err)
	}
}

func TestCursorInBody(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"search","title":"Search","method":"POST","path":"/search",
	  "fields":[{"key":"q","label":"Q","type":"text"}],"body":{"query":"{{ p.q }}"},
	  "pagination":{"type":"cursor","cursor_param":"start_cursor","cursor_path":"next_cursor","param_in":"body","results_path":"results"}}`))
	var bodies []string
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		bodies = append(bodies, string(b))
		if n == 1 {
			fmt.Fprint(w, `{"results":[{"a":1}],"next_cursor":"xyz"}`)
		} else {
			fmt.Fprint(w, `{"results":[{"a":2}],"next_cursor":null}`)
		}
	})
	items, err := runPackageOp(p, map[string]any{"q": "hi"}, cred, nil)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if bodies[1] != `{"query":"hi","start_cursor":"xyz"}` {
		t.Errorf("second body: %s", bodies[1])
	}
}

func TestErrorsNamePackageOpAndField(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"get","title":"Get","method":"GET","path":"/items/{{ p.id }}",
	  "fields":[{"key":"id","label":"Item ID","type":"text","required":true}]}`))
	_, err := runPackageOp(p, map[string]any{}, cred, nil)
	if err == nil || err.Error() != `apipkg demo get: field "id" (Item ID) is required` {
		t.Errorf("required: %v", err)
	}
	_, err = runPackageOp(p, map[string]any{"id": "1"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no credential") || !strings.Contains(err.Error(), "demo") {
		t.Errorf("credential: %v", err)
	}
	_, err = runPackageOp(p, map[string]any{"operation": "nope"}, cred, nil)
	if err == nil || !strings.Contains(err.Error(), `no operation "nope" (have: get)`) {
		t.Errorf("op: %v", err)
	}
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":"not found"}`)
	})
	_, err = runPackageOp(p, map[string]any{"id": "1"}, cred, nil)
	if err == nil || err.Error() != `apipkg demo get: HTTP 404: {"error":"not found"}` {
		t.Errorf("http: %v", err)
	}
}

func TestErrorBodyEchoCapped(t *testing.T) {
	long := strings.Repeat("x", maxErrorBodyEcho+10)
	if got := errorBodyEcho([]byte(long)); !strings.HasSuffix(got, "…(10 bytes truncated)") {
		t.Errorf("echo: %q", got[len(got)-30:])
	}
}

func TestPerItemExpressionsAndDefaults(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"get","title":"Get","method":"GET","path":"/items/{{ p.id }}",
	  "query":{"v":"{{ p.verbose | bool }}","x":"{{ item.extra }}"},
	  "fields":[{"key":"id","label":"ID","type":"text","required":true},{"key":"verbose","label":"Verbose","type":"boolean","default":true}],
	  "output":{"path":"item"}}`))
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		fmt.Fprintf(w, `{"item":{"n":%d}}`, n)
	})
	out, err := (&Node{pkg: p}).Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{
		{JSON: map[string]any{"id": "a", "extra": "e1"}}, {JSON: map[string]any{"id": "b"}},
	}}, map[string]any{"id": "{{ $json.id }}", "credential": cred})
	if err != nil || len(out[0].Items) != 2 || out[0].Items[1].JSON["n"] != 2.0 {
		t.Fatalf("%v %v", out, err)
	}
	r0, r1 := (*seen)[0], (*seen)[1]
	if r0.URL.Path != "/v1/items/a" || r1.URL.Path != "/v1/items/b" {
		t.Errorf("per-item ids: %s %s", r0.URL.Path, r1.URL.Path)
	}
	if r0.URL.Query().Get("v") != "true" || r0.URL.Query().Get("x") != "e1" || r1.URL.Query().Has("x") {
		t.Errorf("query: %v %v", r0.URL.Query(), r1.URL.Query())
	}
	if !contains((&Node{pkg: p}).PerItemConfigFields(), "id") {
		t.Error("PerItemConfigFields must hold back op fields")
	}
}

func TestEmptyResponseAndScalars(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"del","title":"Del","method":"DELETE","path":"/x"},
	  {"resource":"","operation":"one","title":"One","method":"GET","path":"/one","output":{"split":false}}`))
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		if r.URL.Path == "/v1/x" {
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `[1,2]`)
	})
	items, err := runPackageOp(p, map[string]any{"operation": "del"}, cred, nil)
	if err != nil || len(items) != 1 || items[0].JSON["success"] != true {
		t.Errorf("204: %v %v", items, err)
	}
	items, err = runPackageOp(p, map[string]any{"operation": "one"}, cred, nil)
	if err != nil || len(items) != 1 || len(items[0].JSON["data"].([]any)) != 2 {
		t.Errorf("no split: %v %v", items, err)
	}
}

func TestBinaryDownload(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"download","title":"Download","method":"GET","path":"/files/{{ p.id }}",
	  "fields":[{"key":"id","label":"ID","type":"text","required":true},{"key":"binaryPropertyName","label":"Put output in","type":"text","default":"data"}],
	  "binary":{"download":true,"property_field":"binaryPropertyName"}}`))
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		fmt.Fprint(w, "%PDF-1.7")
	})
	items, err := runPackageOp(p, map[string]any{"id": "9", "binaryPropertyName": "pdf"}, cred, nil)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	it := items[0]
	if string(it.Binary["pdf"]) != "%PDF-1.7" || it.JSON["file_name"] != "report.pdf" || it.JSON["mime_type"] != "application/pdf" || it.JSON["size"] != 8 {
		t.Errorf("download item: %v %q", it.JSON, it.Binary)
	}
	if (*seen)[0].Header.Get("Accept") == "application/json" {
		t.Error("a download must not ask for JSON")
	}
}

func TestBinaryUploadErrorsAndBase64(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"up","title":"Up","method":"PUT","path":"/f","body_type":"raw",
	  "fields":[{"key":"binaryPropertyName","label":"From","type":"text","default":"data"}],
	  "binary":{"upload_field":"binaryPropertyName","mime_type":"text/plain"}}`))
	var got []string
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		got = append(got, string(b)+"|"+r.Header.Get("Content-Type"))
		fmt.Fprint(w, `{}`)
	})
	if _, err := runPackageOp(p, map[string]any{}, cred, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), `no binary property "data"`) {
		t.Errorf("missing binary: %v", err)
	}
	// n8n's shape: item.json.binary.<prop>.data (base64).
	if _, err := runPackageOp(p, map[string]any{}, cred, map[string]any{
		"binary": map[string]any{"data": map[string]any{"data": "aGVsbG8="}}}); err != nil {
		t.Fatal(err)
	}
	if got[0] != "hello|text/plain" {
		t.Errorf("upload: %v", got)
	}
}

func TestFormBodyAndHeaders(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"post","title":"Post","method":"POST","path":"/f","body_type":"form",
	  "headers":{"X-Trace":"{{ p.trace }}","X-Empty":"{{ p.none }}"},
	  "fields":[{"key":"a","label":"A","type":"text"},{"key":"trace","label":"T","type":"text"},{"key":"none","label":"N","type":"text"}],
	  "body":{"a":"{{ p.a }}","b":"{{ p.none }}","list":["x","y"]}}`))
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		r.ParseForm()
		fmt.Fprint(w, `{}`)
	})
	if _, err := runPackageOp(p, map[string]any{"a": "1", "trace": "t1"}, cred, nil); err != nil {
		t.Fatal(err)
	}
	r := (*seen)[0]
	if r.PostForm.Get("a") != "1" || r.PostForm.Has("b") || strings.Join(r.PostForm["list"], ",") != "x,y" {
		t.Errorf("form: %v", r.PostForm)
	}
	if r.Header.Get("X-Trace") != "t1" || r.Header.Get("X-Empty") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("headers: %v", r.Header)
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
