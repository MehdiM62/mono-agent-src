package service

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func newAsanaCapture(t *testing.T) *apiCapture {
	t.Helper()
	c := newAPICapture(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/1.0/tasks", "/api/1.0/projects", "/api/1.0/workspaces":
			if r.Method == "GET" {
				return http.StatusOK, `{"data":[{"gid":"1"},{"gid":"2"}]}`
			}
		}
		return http.StatusOK, `{"data":{"gid":"1"}}`
	})
	c.point(t, &asanaBaseURL, "/api/1.0")
	return c
}

func TestAsanaOperationsRequests(t *testing.T) {
	cases := []struct {
		name      string
		config    map[string]interface{}
		method    string
		path      string
		query     map[string]string
		noQuery   []string
		body      map[string]interface{}
		wantItems int
	}{
		{name: "list_tasks by project", config: map[string]interface{}{"operation": "list_tasks", "project_gid": "p1", "workspace_gid": "w1", "assignee": "me"},
			method: "GET", path: "/api/1.0/tasks", query: map[string]string{"project": "p1", "opt_fields": "gid,name,notes,due_on,completed,assignee,created_at,modified_at"},
			noQuery: []string{"workspace", "assignee"}, wantItems: 2},
		{name: "list_tasks by workspace+assignee", config: map[string]interface{}{"operation": "list_tasks", "workspace_gid": "w1", "assignee": "me"},
			method: "GET", path: "/api/1.0/tasks", query: map[string]string{"workspace": "w1", "assignee": "me"}, noQuery: []string{"project"}, wantItems: 2},
		{name: "get_task", config: map[string]interface{}{"operation": "get_task", "task_gid": "t1"},
			method: "GET", path: "/api/1.0/tasks/t1", wantItems: 1},
		{name: "create_task", config: map[string]interface{}{"operation": "create_task", "name": "Write", "notes": "n", "due_on": "2026-10-20", "assignee": "me", "project_gid": "p1", "workspace_gid": "w1"},
			method: "POST", path: "/api/1.0/tasks", wantItems: 1,
			body: map[string]interface{}{"data": map[string]interface{}{
				"name": "Write", "notes": "n", "due_on": "2026-10-20", "assignee": "me", "projects": []interface{}{"p1"}, "workspace": "w1",
			}}},
		{name: "update_task (completed false is sent)", config: map[string]interface{}{"operation": "update_task", "task_gid": "t1", "name": "Renamed", "completed": false},
			method: "PUT", path: "/api/1.0/tasks/t1", wantItems: 1,
			body: map[string]interface{}{"data": map[string]interface{}{"name": "Renamed", "completed": false}}},
		{name: "list_projects", config: map[string]interface{}{"operation": "list_projects", "workspace_gid": "w1"},
			method: "GET", path: "/api/1.0/projects", query: map[string]string{"workspace": "w1"}, wantItems: 2},
		{name: "list_workspaces", config: map[string]interface{}{"operation": "list_workspaces"},
			method: "GET", path: "/api/1.0/workspaces", wantItems: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newAsanaCapture(t)
			items := capRun(t, &AsanaNode{}, workflow.NodeInput{}, capMerge(map[string]interface{}{"access_token": "asana_tok"}, tc.config))
			if len(items) != tc.wantItems {
				t.Errorf("got %d items, want %d", len(items), tc.wantItems)
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer asana_tok" {
				t.Errorf("Authorization = %q", got)
			}
			capWantValues(t, "query", req.Query, tc.query)
			for _, k := range tc.noQuery {
				if req.Query.Has(k) {
					t.Errorf("query has %s, want it absent", k)
				}
			}
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

func TestAsanaValidation(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{"missing token", map[string]interface{}{"operation": "list_workspaces"}, "access_token"},
		{"list_tasks without scope", map[string]interface{}{"access_token": "t", "operation": "list_tasks", "workspace_gid": "w1"}, "project_gid"},
		{"get_task without gid", map[string]interface{}{"access_token": "t", "operation": "get_task"}, "task_gid"},
		{"update_task without gid", map[string]interface{}{"access_token": "t", "operation": "update_task"}, "task_gid"},
		{"unknown operation", map[string]interface{}{"access_token": "t", "operation": "nope"}, "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newAsanaCapture(t)
			capWantErr(t, capRunErr(t, &AsanaNode{}, tc.config), tc.want)
			if n := len(c.all()); n != 0 {
				t.Errorf("sent %d requests, want 0", n)
			}
		})
	}
}
