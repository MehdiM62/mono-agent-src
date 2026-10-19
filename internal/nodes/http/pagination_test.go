package httpnodes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// pagedAPI serves 3 pages of 2 items for ?page=1..3 (empty after).
func pagedAPI(t *testing.T) *recorder {
	return newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		if page < 1 || page > 3 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		fmt.Fprintf(w, `[{"id":%d},{"id":%d}]`, page*10+1, page*10+2)
	})
}

func ids(items []workflow.Item) string {
	var s []string
	for _, it := range items {
		s = append(s, fmt.Sprint(it.JSON["id"]))
	}
	return strings.Join(s, ",")
}

func TestPaginationUpdateParameterUntilEmpty(t *testing.T) {
	rec := pagedAPI(t)
	m := mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "full_response": false,
		"pagination_type": "updateAParameterInEachRequest",
		"pagination_parameters": []interface{}{
			map[string]interface{}{"type": "qs", "name": "page", "value": "{{ add $pageCount 1 }}"},
		},
	})
	if got := ids(m); got != "11,12,21,22,31,32" {
		t.Fatalf("ids = %s", got)
	}
	if n := len(rec.all()); n != 4 {
		t.Fatalf("%d requests, want 3 pages + the empty one", n)
	}
}

func TestPaginationHeaderAndBodyParameters(t *testing.T) {
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		cursor := r.Header.Get("X-Cursor")
		w.Header().Set("Content-Type", "application/json")
		if cursor == "c2" {
			fmt.Fprintf(w, `{"items":[], "next":""}`)
			return
		}
		fmt.Fprint(w, `{"items":[{"id":1}],"next":"c2"}`)
	})
	m := mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "method": "POST", "body": map[string]interface{}{"filter": "open"},
		"pagination_type":                "update_parameter",
		"pagination_complete_when":       "expression",
		"pagination_complete_expression": `{{ eq $response.body.next "" }}`,
		"pagination_parameters": `[{"type":"headers","name":"X-Cursor","value":"{{ if $response.body.next }}{{ $response.body.next }}{{ else }}c1{{ end }}"},
		                           {"type":"body","name":"page","value":"{{ $pageCount }}"}]`,
	})
	reqs := rec.all()
	if len(reqs) != 2 || reqs[0].Header.Get("X-Cursor") != "c1" || reqs[1].Header.Get("X-Cursor") != "c2" {
		t.Fatalf("cursors: %v", reqs)
	}
	if !strings.Contains(reqs[1].Body, `"page":"1"`) || !strings.Contains(reqs[1].Body, `"filter":"open"`) {
		t.Fatalf("second body = %s", reqs[1].Body)
	}
	if len(m) != 2 {
		t.Fatalf("want both pages emitted, got %d", len(m))
	}
}

func TestPaginationNextURL(t *testing.T) {
	var base string
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/a":
			fmt.Fprintf(w, `{"data":[{"id":1}],"next":%q}`, base+"/b?cursor=x")
		case "/b":
			fmt.Fprint(w, `{"data":[{"id":2}],"next":"/c"}`)
		default:
			fmt.Fprint(w, `{"data":[{"id":3}],"next":null}`)
		}
	})
	base = rec.URL
	m := mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL + "/a", "query_params": map[string]interface{}{"cursor": "start", "limit": "1"},
		"pagination_type": "responseContainsNextURL", "pagination_next_url": "{{ $response.body.next }}",
		"auth_type": "api_key", "auth_api_key_in": "query", "auth_api_key_name": "key", "auth_api_key_value": "k",
	})
	if len(m) != 3 {
		t.Fatalf("pages = %d", len(m))
	}
	reqs := rec.all()
	if reqs[1].Path != "/b" || reqs[1].Query["cursor"][0] != "x" || reqs[1].Query["limit"][0] != "1" || reqs[2].Path != "/c" || reqs[2].Query["key"][0] != "k" {
		t.Fatalf("requests: %+v", reqs)
	}
}

func TestPaginationStatusCodesAndMaxPages(t *testing.T) {
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") == "2" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"p":%q}`, r.URL.Query().Get("p"))
	})
	cfg := map[string]interface{}{
		"url": rec.URL, "pagination_type": "update_parameter",
		"pagination_parameters":    []interface{}{map[string]interface{}{"name": "p", "value": "{{ $pageCount }}"}},
		"pagination_complete_when": "receiveSpecificStatusCodes", "pagination_status_codes": "404, 410",
	}
	m, e := execute(t, &RequestNode{}, cfg)
	if len(m) != 2 || len(e) != 0 {
		t.Fatalf("want pages 0 and 1 then stop on 404: %v %v", m, e)
	}

	cfg["pagination_complete_when"] = "response_is_empty"
	cfg["max_pages"] = 1
	m, _ = execute(t, &RequestNode{}, cfg)
	if len(m) != 1 {
		t.Fatalf("max_pages 1 gave %d pages", len(m))
	}

	// A 404 with response_is_empty is an error item.
	delete(cfg, "max_pages")
	m, e = execute(t, &RequestNode{}, cfg)
	if len(m) != 2 || len(e) != 1 || e[0].JSON["status"] != 404 {
		t.Fatalf("got %v %v", m, e)
	}
}

// A server that ignores the page parameter repeats itself: stop.
func TestPaginationStopsOnRepeatedResponse(t *testing.T) {
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"same":true}`) })
	m := mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "pagination_type": "update_parameter",
		"pagination_parameters": []interface{}{map[string]interface{}{"name": "p", "value": "{{ $pageCount }}"}},
	})
	if len(m) != 1 || len(rec.all()) != 2 {
		t.Fatalf("items %d requests %d", len(m), len(rec.all()))
	}
}

func TestPaginationConfigErrors(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"pagination_type": "next_url"},
		{"pagination_type": "pages"},
		{"pagination_type": "update_parameter", "pagination_complete_when": "status_codes"},
		{"pagination_type": "update_parameter", "pagination_complete_when": "expression"},
	} {
		cfg["url"] = "http://127.0.0.1:1"
		if _, err := (&RequestNode{}).Execute(t.Context(), workflow.NodeInput{}, cfg); err == nil || !strings.Contains(err.Error(), "pagination") {
			t.Errorf("%v: err = %v", cfg, err)
		}
	}
	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": "http://127.0.0.1:1", "pagination_type": "update_parameter"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "pagination_parameters") {
		t.Fatalf("missing parameters: %v", e)
	}
}

// The original offset mode keeps working as before.
func TestPaginationLegacyOffset(t *testing.T) {
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		p, _ := strconv.Atoi(r.URL.Query().Get("pg"))
		if p >= 2 {
			fmt.Fprint(w, `[{"id":99}]`)
			return
		}
		fmt.Fprintf(w, `[{"id":%d},{"id":%d}]`, p*2, p*2+1)
	})
	m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "pagination_type": "offset", "page_field": "pg", "page_size": 2})
	if got := ids(m); got != "0,1,2,3,99" {
		t.Fatalf("ids = %s", got)
	}
	if rec.all()[0].Query["per_page"][0] != "2" {
		t.Fatal("per_page not sent")
	}
}

// The pagination templates are held back from the engine's own pass.
func TestPaginationTemplatesAreHeldBack(t *testing.T) {
	held := strings.Join((&RequestNode{}).PerItemConfigFields(), ",")
	for _, k := range []string{"pagination_next_url", "pagination_parameters", "pagination_complete_expression", "url"} {
		if !strings.Contains(held, k) {
			t.Errorf("%s is not held back", k)
		}
	}
}

// A next page on another host, which the response names, goes without the
// request's auth, as a redirect there does; send_credentials_on_redirect
// sends it on.
func TestPaginationNextURLOnAnotherHostGoesWithoutAuth(t *testing.T) {
	other := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":2}],"next":null}`)
	})
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":1}],"next":%q}`, other.URL+"/p2")
	})
	for _, keep := range []bool{false, true} {
		mustOK(t, &RequestNode{}, map[string]interface{}{
			"url": rec.URL + "/a", "pagination_type": "responseContainsNextURL", "pagination_next_url": "{{ $response.body.next }}",
			"auth_type": "bearer", "auth_token": "tok-1", "send_credentials_on_redirect": keep,
		})
		first, next := rec.all(), other.all()
		if got := first[len(first)-1].Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("keep %v: the first page's Authorization = %q", keep, got)
		}
		want := ""
		if keep {
			want = "Bearer tok-1"
		}
		if got := next[len(next)-1].Header.Get("Authorization"); got != want {
			t.Errorf("keep %v: the other host's page got Authorization %q, want %q", keep, got, want)
		}
	}
}
