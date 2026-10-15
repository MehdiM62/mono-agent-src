package service

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func newSheetsCapture(t *testing.T) *apiCapture {
	t.Helper()
	c := newAPICapture(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v4/spreadsheets" {
			return http.StatusOK, `{"spreadsheetId":"new_sheet"}`
		}
		return http.StatusOK, `{"updatedRange":"Sheet1!A1:B2"}`
	})
	c.point(t, &sheetsBaseURL, "/v4/spreadsheets")
	return c
}

func TestGoogleSheetsWriteOperationsRequests(t *testing.T) {
	values := []interface{}{[]interface{}{"a", float64(1)}, []interface{}{"b", float64(2)}}
	cases := []struct {
		name   string
		config map[string]interface{}
		input  workflow.NodeInput
		method string
		path   string
		query  map[string]string
		body   map[string]interface{}
	}{
		{name: "append_rows explicit values", config: map[string]interface{}{"operation": "append_rows", "values": values},
			method: "POST", path: "/v4/spreadsheets/sheet_1/values/Sheet1:append", query: map[string]string{"valueInputOption": "RAW"},
			body: map[string]interface{}{"values": values}},
		{name: "append_rows from input items with header", config: map[string]interface{}{
			"operation": "append_rows", "sheet_name": "Leads", "range": "A1", "use_header_row": true, "value_input_option": "USER_ENTERED"},
			input:  workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"name": "Ann"})}},
			method: "POST", path: "/v4/spreadsheets/sheet_1/values/Leads!A1:append", query: map[string]string{"valueInputOption": "USER_ENTERED"},
			body: map[string]interface{}{"values": []interface{}{[]interface{}{"name"}, []interface{}{"Ann"}}}},
		{name: "update_rows", config: map[string]interface{}{"operation": "update_rows", "sheet": "Data", "range": "A2:B3", "values": values},
			method: "PUT", path: "/v4/spreadsheets/sheet_1/values/Data!A2:B3", query: map[string]string{"valueInputOption": "RAW"},
			body: map[string]interface{}{"range": "Data!A2:B3", "values": values}},
		{name: "clear_range", config: map[string]interface{}{"operation": "clear_range", "range": "A1:Z100"},
			method: "POST", path: "/v4/spreadsheets/sheet_1/values/Sheet1!A1:Z100:clear",
			body: map[string]interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSheetsCapture(t)
			cfg := capMerge(map[string]interface{}{"access_token": "ya29", "spreadsheet_id": "sheet_1"}, tc.config)
			items := capRun(t, &GoogleSheetsNode{}, tc.input, cfg)
			if len(items) != 1 {
				t.Errorf("got %d items, want 1", len(items))
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer ya29" {
				t.Errorf("Authorization = %q", got)
			}
			capWantValues(t, "query", req.Query, tc.query)
			if got := req.json(t); !reflect.DeepEqual(got, tc.body) {
				t.Errorf("body = %v, want %v", got, tc.body)
			}
		})
	}
}

func TestGoogleSheetsAppendAutoCreatesSpreadsheet(t *testing.T) {
	c := newSheetsCapture(t)
	items := capRun(t, &GoogleSheetsNode{}, workflow.NodeInput{}, map[string]interface{}{
		"access_token": "ya29", "operation": "append_rows", "spreadsheet_id": "new", "title": "Leads 2026",
		"values": []interface{}{[]interface{}{"x"}},
	})
	reqs := c.all()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2 (create + append)", len(reqs))
	}
	if reqs[0].Method != "POST" || reqs[0].Path != "/v4/spreadsheets" {
		t.Errorf("create request = %s %s", reqs[0].Method, reqs[0].Path)
	}
	wantCreate := map[string]interface{}{"properties": map[string]interface{}{"title": "Leads 2026"}}
	if got := reqs[0].json(t); !reflect.DeepEqual(got, wantCreate) {
		t.Errorf("create body = %v, want %v", got, wantCreate)
	}
	if reqs[1].Path != "/v4/spreadsheets/new_sheet/values/Sheet1:append" {
		t.Errorf("append path = %q", reqs[1].Path)
	}
	if len(items) != 1 || items[0].JSON["spreadsheet_id"] != "new_sheet" || items[0].JSON["rows_written"] != 1 {
		t.Errorf("items = %+v", items)
	}
}

func TestGoogleSheetsUpdateRowsRejectsUnresolvedRange(t *testing.T) {
	c := newSheetsCapture(t)
	err := capRunErr(t, &GoogleSheetsNode{}, map[string]interface{}{
		"access_token": "ya29", "spreadsheet_id": "s", "operation": "update_rows", "range": "A<no value>",
	})
	capWantErr(t, err, "range", "unresolved")
	if n := len(c.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}
