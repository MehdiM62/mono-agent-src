package apipkg

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func convertSpec(t *testing.T, file string, opts OpenAPIOptions) (*Package, []string) {
	t.Helper()
	b, err := os.ReadFile("testdata/openapi/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var warns []string
	opts.Warn = func(s string) { warns = append(warns, s) }
	p, err := OpenAPIToPackage(b, opts)
	if err != nil {
		t.Fatalf("convert %s: %v", file, err)
	}
	// The written form round-trips through Parse.
	out, _ := json.MarshalIndent(p, "", "  ")
	if _, err := Parse(out, file); err != nil {
		t.Fatalf("converted %s does not parse: %v", file, err)
	}
	return p, warns
}

func mustOp(t *testing.T, p *Package, key string) *Operation {
	t.Helper()
	op, ok := opByKey(p, key)
	if !ok {
		var have []string
		for _, o := range p.Operations {
			have = append(have, o.Key())
		}
		t.Fatalf("no op %q; have %v", key, have)
	}
	return op
}

func TestOpenAPIPetstoreYAML(t *testing.T) {
	p, warns := convertSpec(t, "petstore.yaml", OpenAPIOptions{ID: "petstore"})
	if p.BaseURL != "https://petstore3.swagger.io/api/v3" || p.NodeType != "app.petstore" || p.Title != "Swagger Petstore" {
		t.Fatalf("header: %+v", p)
	}
	if p.Description != "A sample pet store server." || p.DocsURL != "https://swagger.io" {
		t.Errorf("description/docs: %q %q", p.Description, p.DocsURL)
	}
	if got := resourceList(p.Resources); got != "pet, store" {
		t.Errorf("resources: %s", got)
	}
	if len(p.Operations) != 9 {
		t.Errorf("ops: %d", len(p.Operations))
	}
	if len(p.Credentials) != 2 || p.Credentials[0].Platform != "petstore" || p.Credentials[0].Inject.Headers["api_key"] != "{{ cred.api_key }}" {
		t.Fatalf("credentials: %+v", p.Credentials)
	}
	if c := p.Credentials[1]; c.Platform != "petstore_petstore_auth" || c.Methods[0] != "bearer" {
		t.Errorf("implicit oauth2 should fall back to bearer: %+v", c)
	}
	if !strings.Contains(strings.Join(warns, "\n"), "petstore_auth") {
		t.Errorf("warnings: %v", warns)
	}
	find := mustOp(t, p, "pet.findPetsByStatus")
	if f := find.Fields[0]; f.Type != "select" || f.Default != "available" || len(f.Options) != 3 {
		t.Errorf("status field: %+v", f)
	}
	if get := mustOp(t, p, "pet.getPetById"); get.Path != "/pet/{{ p.petId }}" || !get.Fields[0].Required || get.Fields[0].Type != "number" {
		t.Errorf("getPetById: %+v", get)
	}

	cred := map[string]any{"api_key": "k-123"}
	srv := newMockServer(t, map[string]any{"id": 7}, 200)
	items, err := runPackageOp(p, map[string]any{"resource": "pet", "operation": "addPet",
		"name": "doggie", "photoUrls": []any{"u1"}, "status": "sold", "category": map[string]any{"id": 1}}, cred, nil)
	if err != nil || len(items) != 1 || items[0].JSON["id"] != float64(7) {
		t.Fatalf("addPet: %v %v", items, err)
	}
	r := srv.requests()[0]
	if r.method != "POST" || r.path != "/api/v3/pet" || r.header.Get("api_key") != "k-123" {
		t.Errorf("addPet request: %s %s %v", r.method, r.path, r.header)
	}
	var body map[string]any
	_ = json.Unmarshal(r.body, &body)
	if body["name"] != "doggie" || body["status"] != "sold" || body["category"] == nil || body["id"] != nil {
		t.Errorf("addPet body: %s", r.body)
	}
	if _, err := runPackageOp(p, map[string]any{"resource": "pet", "operation": "addPet", "name": "x"}, cred, nil); err == nil ||
		!strings.Contains(err.Error(), `"photoUrls"`) {
		t.Errorf("required photoUrls not enforced: %v", err)
	}

	srv = newMockServer(t, []any{}, 200)
	if _, err := runPackageOp(p, map[string]any{"resource": "pet", "operation": "findPetsByTags", "tags": []any{"a", "b"}}, cred, nil); err != nil {
		t.Fatal(err)
	}
	if q := srv.requests()[0].query["tags"]; strings.Join(q, ",") != "a,b" || len(q) != 2 {
		t.Errorf("exploded array query: %v", q)
	}

	// uploadFile: octet-stream body from the item's binary data.
	up := mustOp(t, p, "pet.uploadFile")
	if up.BodyType != BodyRaw || up.Binary == nil || up.Binary.UploadField != "binaryPropertyName" {
		t.Fatalf("uploadFile: %+v", up)
	}
	srv = newMockServer(t, map[string]any{"code": 200}, 200)
	out, err := run(context.Background(), p, workflow.NodeInput{Items: []workflow.Item{{
		JSON: map[string]any{}, Binary: map[string][]byte{"photo": []byte("PNGDATA")}}}},
		map[string]any{"resource": "pet", "operation": "uploadFile", "petId": 7, "binaryPropertyName": "photo",
			"additionalMetadata": "front", "credential": cred}, true)
	if err != nil || len(out[0].Items) != 1 {
		t.Fatalf("uploadFile: %v %v", out, err)
	}
	r = srv.requests()[0]
	if r.path != "/api/v3/pet/7/uploadImage" || string(r.body) != "PNGDATA" || r.header.Get("Content-Type") != "application/octet-stream" ||
		r.query.Get("additionalMetadata") != "front" {
		t.Errorf("upload request: %s %q %v %v", r.path, r.body, r.header, r.query)
	}
}

func TestOpenAPINotes31JSON(t *testing.T) {
	p, _ := convertSpec(t, "notes.json", OpenAPIOptions{})
	if p.ID != "notes_service" || p.BaseURL != "https://eu.notes.example.com/v2" {
		t.Fatalf("id/base: %s %s", p.ID, p.BaseURL)
	}
	if got := resourceList(p.Resources); got != "attachments, notes" {
		t.Errorf("resources: %s", got)
	}
	if c := p.Credentials[1]; c.Platform != "notes_service_oauth" || c.OAuth == nil ||
		c.OAuth.TokenURL != "https://auth.notes.example.com/token" || strings.Join(c.OAuth.Scopes, " ") != "notes:read notes:write" {
		t.Errorf("oauth credential: %+v", c)
	}
	list := mustOp(t, p, "notes.listNotes")
	if list.Query["labels"] != "{{ p.labels | csv }}" || list.Headers["X-Request-Id"] != "{{ p.xRequestId }}" {
		t.Errorf("listNotes: %+v", list)
	}
	srv := newMockServer(t, []any{map[string]any{"id": "n1"}, map[string]any{"id": "n2"}}, 200)
	items, err := runPackageOp(p, map[string]any{"resource": "notes", "operation": "listNotes", "labels": []any{"x", "y"}, "xRequestId": "r1"},
		map[string]any{"access_token": "tok"}, nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("listNotes: %v %v", items, err)
	}
	r := srv.requests()[0]
	if r.path != "/v2/notes" || r.query.Get("labels") != "x,y" || r.query.Get("page_size") != "20" ||
		r.header.Get("X-Request-Id") != "r1" || r.header.Get("Authorization") != "Bearer tok" {
		t.Errorf("listNotes request: %s %v %v", r.path, r.query, r.header)
	}

	create := mustOp(t, p, "notes.createNote")
	req := map[string]bool{}
	for _, f := range create.Fields {
		req[f.Key] = f.Required
	}
	if !req["title"] || !req["body"] || req["pinned"] || req["color"] {
		t.Errorf("allOf required: %v", req)
	}
	if upd := mustOp(t, p, "notes.updateNote"); upd.Body != "{{ p.body }}" || upd.BodyType != BodyJSON {
		t.Errorf("updateNote: %+v", upd)
	}

	upl := mustOp(t, p, "attachments.uploadAttachment")
	if upl.BodyType != BodyMultipart || upl.Binary == nil || upl.Binary.MultipartField != "file" {
		t.Fatalf("uploadAttachment: %+v", upl)
	}
	srv = newMockServer(t, map[string]any{"id": "a1"}, 200)
	_, err = run(context.Background(), p, workflow.NodeInput{Items: []workflow.Item{{
		JSON: map[string]any{"file_name": "cat.png"}, Binary: map[string][]byte{"data": []byte("IMG")}}}},
		map[string]any{"resource": "attachments", "operation": "uploadAttachment", "noteId": "n1", "caption": "a cat",
			"credential": map[string]any{"access_token": "tok"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	checkBody(t, "uploadAttachment", srv.requests()[0], map[string]any{"caption": "a cat", "file": "file:cat.png:IMG"})

	dl := mustOp(t, p, "attachments.downloadAttachment")
	if dl.Binary == nil || !dl.Binary.Download {
		t.Fatalf("downloadAttachment: %+v", dl)
	}
}

func TestOpenAPISwagger2(t *testing.T) {
	p, _ := convertSpec(t, "files-swagger2.json", OpenAPIOptions{ID: "file_drop"})
	if p.BaseURL != "https://files.example.com/api" || len(p.Resources) != 0 {
		t.Fatalf("base/resources: %s %v", p.BaseURL, p.Resources)
	}
	if c := p.Credentials[1]; c.Platform != "file_drop_key" || c.Inject.Query["apikey"] != "{{ cred.api_key }}" {
		t.Errorf("apiKey credential: %+v", c)
	}
	if c := p.Credentials[0]; c.Inject.Basic == nil {
		t.Errorf("basic credential: %+v", c)
	}
	if up := mustOp(t, p, "uploadFile"); up.BodyType != BodyMultipart || up.Binary.MultipartField != "content" {
		t.Errorf("uploadFile: %+v", up)
	}
	if dl := mustOp(t, p, "downloadFile"); dl.Binary == nil || !dl.Binary.Download {
		t.Errorf("downloadFile: %+v", dl)
	}
	srv := newMockServer(t, map[string]any{"id": "f1"}, 200)
	if _, err := runPackageOp(p, map[string]any{"operation": "createFolder", "name": "Docs", "shared": true},
		map[string]any{"api_key": "K"}, nil); err != nil {
		t.Fatal(err)
	}
	r := srv.requests()[0]
	if r.method != "POST" || r.path != "/api/folders" || r.query.Get("apikey") != "K" || string(r.body) != `{"name":"Docs","shared":true}` {
		t.Errorf("createFolder: %s %s %v %s", r.method, r.path, r.query, r.body)
	}
	srv = newMockServer(t, []any{}, 200)
	if _, err := runPackageOp(p, map[string]any{"operation": "listFolders", "ids": []any{"1", "2"}},
		map[string]any{"username": "u", "password": "pw"}, nil); err != nil {
		t.Fatal(err)
	}
	r = srv.requests()[0]
	if r.query.Get("ids") != "1,2" || !strings.HasPrefix(r.header.Get("Authorization"), "Basic ") {
		t.Errorf("listFolders: %v %v", r.query, r.header)
	}
}

func TestOpenAPITagFilterAndErrors(t *testing.T) {
	p, _ := convertSpec(t, "petstore.yaml", OpenAPIOptions{ID: "petstore", Tags: []string{"store"}})
	if len(p.Operations) != 2 || resourceList(p.Resources) != "store" {
		t.Errorf("--ops store: %d ops, resources %s", len(p.Operations), resourceList(p.Resources))
	}
	b, _ := os.ReadFile("testdata/openapi/petstore.yaml")
	if _, err := OpenAPIToPackage(b, OpenAPIOptions{ID: "petstore", Tags: []string{"nope"}}); err == nil {
		t.Error("a tag filter matching nothing should fail")
	}
	if _, err := OpenAPIToPackage([]byte(`{"openapi":"2.5"}`), OpenAPIOptions{}); err == nil {
		t.Error("unknown version should fail")
	}
	if _, err := OpenAPIToPackage(b, OpenAPIOptions{ID: "Bad-Id"}); err == nil {
		t.Error("bad id should fail")
	}
	if p, _ := convertSpec(t, "petstore.yaml", OpenAPIOptions{ID: "github"}); p.Credentials[0].Platform != "github_api" {
		t.Errorf("a built-in platform id must not be reused: %s", p.Credentials[0].Platform)
	}
}
