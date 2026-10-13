package service

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

const salesforceTestResponse = `{"id":"003A","success":true,"records":[{"Id":"003A"}],"done":true}`

func newSalesforceCapture(t *testing.T) *apiCapture {
	t.Helper()
	return newAPICapture(t, func(*http.Request) (int, string) { return http.StatusOK, salesforceTestResponse })
}

func TestSalesforceOperationsRequests(t *testing.T) {
	const v = "/services/data/v" + salesforceDefaultAPIVersion
	fields := map[string]interface{}{"LastName": "Doe", "Email": "d@x.co"}
	cases := []struct {
		name     string
		config   map[string]interface{}
		method   string
		path     string
		query    map[string]string
		body     map[string]interface{}
		wantItem map[string]interface{}
	}{
		{name: "query", config: map[string]interface{}{"operation": "query", "soql": "SELECT Id FROM Contact WHERE Name = 'A&B'"},
			method: "GET", path: v + "/query", query: map[string]string{"q": "SELECT Id FROM Contact WHERE Name = 'A&B'"},
			wantItem: map[string]interface{}{"Id": "003A"}},
		{name: "get_record", config: map[string]interface{}{"operation": "get_record", "object_type": "Contact", "record_id": "003A"},
			method: "GET", path: v + "/sobjects/Contact/003A"},
		{name: "create_record", config: map[string]interface{}{"operation": "create_record", "object_type": "Contact", "fields": fields},
			method: "POST", path: v + "/sobjects/Contact", body: fields},
		{name: "update_record", config: map[string]interface{}{"operation": "update_record", "object_type": "Contact", "record_id": "003A", "fields": fields},
			method: "PATCH", path: v + "/sobjects/Contact/003A", body: fields,
			wantItem: map[string]interface{}{"id": "003A", "updated": true}},
		{name: "delete_record", config: map[string]interface{}{"operation": "delete_record", "object_type": "Contact", "record_id": "003A"},
			method: "DELETE", path: v + "/sobjects/Contact/003A",
			wantItem: map[string]interface{}{"id": "003A", "deleted": true}},
		{name: "describe_object", config: map[string]interface{}{"operation": "describe_object", "object_type": "Account"},
			method: "GET", path: v + "/sobjects/Account/describe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSalesforceCapture(t)
			cfg := capMerge(map[string]interface{}{"instance_url": c.srv.URL, "access_token": "00Dtok"}, tc.config)
			items := capRun(t, &SalesforceNode{}, workflow.NodeInput{}, cfg)
			if len(items) != 1 {
				t.Fatalf("got %d items, want 1", len(items))
			}
			for k, want := range tc.wantItem {
				if items[0].JSON[k] != want {
					t.Errorf("item[%s] = %v, want %v", k, items[0].JSON[k], want)
				}
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer 00Dtok" {
				t.Errorf("Authorization = %q, want Bearer 00Dtok", got)
			}
			capWantValues(t, "query", req.Query, tc.query)
			if tc.body == nil {
				if len(req.Body) != 0 {
					t.Errorf("body = %q, want none", req.Body)
				}
			} else if got := req.json(t); !reflect.DeepEqual(got, tc.body) {
				t.Errorf("body = %v, want %v", got, tc.body)
			}
		})
	}
}

func TestSalesforceQueryFollowsNextRecordsURL(t *testing.T) {
	c := newAPICapture(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/query/01g-2000") {
			return http.StatusOK, `{"records":[{"Id":"2"}],"done":true}`
		}
		return http.StatusOK, `{"records":[{"Id":"1"}],"done":false,"nextRecordsUrl":"/services/data/v66.0/query/01g-2000"}`
	})
	items := capRun(t, &SalesforceNode{}, workflow.NodeInput{}, map[string]interface{}{
		"instance_url": c.srv.URL + "/", "access_token": "tok", "operation": "query", "soql": "SELECT Id FROM Lead",
	})
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	reqs := c.all()
	if len(reqs) != 2 || reqs[0].Path != "/services/data/v66.0/query" || reqs[1].Path != "/services/data/v66.0/query/01g-2000" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestSalesforceAPIVersion(t *testing.T) {
	cases := []struct {
		version  interface{}
		wantPath string
	}{
		{nil, "/services/data/v66.0/sobjects/Account/describe"},
		{"", "/services/data/v66.0/sobjects/Account/describe"},
		{"60.0", "/services/data/v60.0/sobjects/Account/describe"},
		{"v61.0", "/services/data/v61.0/sobjects/Account/describe"},
		{" 58.0 ", "/services/data/v58.0/sobjects/Account/describe"},
	}
	for _, tc := range cases {
		c := newSalesforceCapture(t)
		cfg := map[string]interface{}{"instance_url": c.srv.URL, "access_token": "tok", "operation": "describe_object", "object_type": "Account"}
		if tc.version != nil {
			cfg["api_version"] = tc.version
		}
		capRun(t, &SalesforceNode{}, workflow.NodeInput{}, cfg)
		if got := c.only(t).Path; got != tc.wantPath {
			t.Errorf("api_version %v: path = %q, want %q", tc.version, got, tc.wantPath)
		}
	}
}

func TestSalesforceInvalidAPIVersion(t *testing.T) {
	for _, bad := range []string{"66", "v66", "abc", "66.0.1", "vv66.0", "66.0/../x"} {
		c := newSalesforceCapture(t)
		err := capRunErr(t, &SalesforceNode{}, map[string]interface{}{
			"instance_url": c.srv.URL, "access_token": "tok", "operation": "describe_object", "object_type": "Account", "api_version": bad,
		})
		capWantErr(t, err, "api_version", bad)
		if n := len(c.all()); n != 0 {
			t.Errorf("api_version %q: sent %d requests, want 0", bad, n)
		}
	}
}
