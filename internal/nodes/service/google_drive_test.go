package service

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func newDriveCapture(t *testing.T) *apiCapture {
	t.Helper()
	c := newAPICapture(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Query().Get("alt") == "media":
			return http.StatusOK, "file-bytes"
		case r.Method == "DELETE":
			return http.StatusNoContent, ""
		case r.URL.Path == "/drive/v3/files" && r.Method == "GET":
			return http.StatusOK, `{"files":[{"id":"f1"},{"id":"f2"}]}`
		}
		return http.StatusOK, `{"id":"f1"}`
	})
	c.point(t, &driveBaseURL, "/drive/v3")
	c.point(t, &driveUploadURL, "/upload/drive/v3")
	return c
}

func TestGoogleDriveOperationsRequests(t *testing.T) {
	cases := []struct {
		name      string
		config    map[string]interface{}
		method    string
		path      string
		query     map[string]string
		body      map[string]interface{}
		wantItems int
	}{
		{name: "list_files", config: map[string]interface{}{"operation": "list_files", "page_size": float64(25), "query": "name contains 'a&b'"},
			method: "GET", path: "/drive/v3/files", wantItems: 2,
			query: map[string]string{"pageSize": "25", "q": "name contains 'a&b'", "fields": "files(id,name,mimeType,size,createdTime,modifiedTime,parents)"}},
		{name: "list_files default page size", config: map[string]interface{}{"operation": "list_files"},
			method: "GET", path: "/drive/v3/files", wantItems: 2, query: map[string]string{"pageSize": "10", "q": ""}},
		{name: "get_file", config: map[string]interface{}{"operation": "get_file", "file_id": "f1"},
			method: "GET", path: "/drive/v3/files/f1", wantItems: 1,
			query: map[string]string{"fields": "id,name,mimeType,size,createdTime,modifiedTime,parents,webViewLink,webContentLink"}},
		{name: "create_folder", config: map[string]interface{}{"operation": "create_folder", "file_name": "Reports", "parent_folder_id": "root1"},
			method: "POST", path: "/drive/v3/files", wantItems: 1,
			body: map[string]interface{}{"name": "Reports", "mimeType": "application/vnd.google-apps.folder", "parents": []interface{}{"root1"}}},
		{name: "delete_file", config: map[string]interface{}{"operation": "delete_file", "file_id": "f1"},
			method: "DELETE", path: "/drive/v3/files/f1", wantItems: 1},
		{name: "share_file", config: map[string]interface{}{"operation": "share_file", "file_id": "f1"},
			method: "POST", path: "/drive/v3/files/f1/permissions", wantItems: 1,
			body: map[string]interface{}{"role": "reader", "type": "anyone"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newDriveCapture(t)
			items := capRun(t, &GoogleDriveNode{}, workflow.NodeInput{}, capMerge(map[string]interface{}{"access_token": "ya29"}, tc.config))
			if len(items) != tc.wantItems {
				t.Errorf("got %d items, want %d", len(items), tc.wantItems)
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer ya29" {
				t.Errorf("Authorization = %q", got)
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

func TestGoogleDriveDownloadFile(t *testing.T) {
	c := newDriveCapture(t)
	items := capRun(t, &GoogleDriveNode{}, workflow.NodeInput{}, map[string]interface{}{
		"access_token": "ya29", "operation": "download_file", "file_id": "f9",
	})
	req := c.only(t)
	if req.Method != "GET" || req.Path != "/drive/v3/files/f9" || req.Query.Get("alt") != "media" {
		t.Errorf("request = %s %s?%s", req.Method, req.Path, req.Query.Encode())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ya29" {
		t.Errorf("Authorization = %q", got)
	}
	if len(items) != 1 || string(items[0].Binary["data"]) != "file-bytes" || items[0].JSON["file_id"] != "f9" {
		t.Errorf("items = %+v", items)
	}
}

func TestGoogleDriveUploadFileMultipart(t *testing.T) {
	c := newDriveCapture(t)
	path := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := capRun(t, &GoogleDriveNode{}, workflow.NodeInput{}, map[string]interface{}{
		"access_token": "ya29", "operation": "upload_file", "file_path": path,
		"mime_type": "text/csv", "parent_folder_id": "folder1",
	})
	if len(items) != 1 || items[0].JSON["id"] != "f1" {
		t.Errorf("items = %+v", items)
	}
	req := c.only(t)
	if req.Method != "POST" || req.Path != "/upload/drive/v3/files" || req.Query.Get("uploadType") != "multipart" {
		t.Errorf("request = %s %s?%s", req.Method, req.Path, req.Query.Encode())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ya29" {
		t.Errorf("Authorization = %q", got)
	}
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/related" {
		t.Fatalf("Content-Type = %q (%v), want multipart/related", req.Header.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])
	meta, err := mr.NextPart()
	if err != nil {
		t.Fatalf("metadata part: %v", err)
	}
	var gotMeta map[string]interface{}
	if err := json.NewDecoder(meta).Decode(&gotMeta); err != nil {
		t.Fatalf("decoding metadata: %v", err)
	}
	wantMeta := map[string]interface{}{"name": "report.csv", "parents": []interface{}{"folder1"}}
	if !reflect.DeepEqual(gotMeta, wantMeta) {
		t.Errorf("metadata = %v, want %v", gotMeta, wantMeta)
	}
	file, err := mr.NextPart()
	if err != nil {
		t.Fatalf("file part: %v", err)
	}
	if ct := file.Header.Get("Content-Type"); ct != "text/csv" {
		t.Errorf("file part Content-Type = %q, want text/csv", ct)
	}
	content, _ := io.ReadAll(file)
	if string(content) != "a,b\n1,2\n" {
		t.Errorf("file content = %q", content)
	}
}

func TestGoogleDriveValidation(t *testing.T) {
	for _, op := range []string{"get_file", "download_file", "delete_file", "share_file"} {
		c := newDriveCapture(t)
		capWantErr(t, capRunErr(t, &GoogleDriveNode{}, map[string]interface{}{"access_token": "t", "operation": op}), "file_id")
		if n := len(c.all()); n != 0 {
			t.Errorf("%s: sent %d requests, want 0", op, n)
		}
	}
	capWantErr(t, capRunErr(t, &GoogleDriveNode{}, map[string]interface{}{"access_token": "t", "operation": "create_folder"}), "file_name")
	capWantErr(t, capRunErr(t, &GoogleDriveNode{}, map[string]interface{}{"access_token": "t", "operation": "upload_file"}), "file_path")
	capWantErr(t, capRunErr(t, &GoogleDriveNode{}, map[string]interface{}{"operation": "list_files"}), "access_token")
}

func TestGoogleDriveHTTPErrorSurfacesStatus(t *testing.T) {
	c := newAPICapture(t, func(*http.Request) (int, string) { return http.StatusForbidden, `{"error":"insufficientPermissions"}` })
	c.point(t, &driveBaseURL, "/drive/v3")
	err := capRunErr(t, &GoogleDriveNode{}, map[string]interface{}{"access_token": "t", "operation": "get_file", "file_id": "f"})
	capWantErr(t, err, "403", "insufficientPermissions")
}
