package apipkg

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/workflow"
)

func TestRegisterAndNodeSchema(t *testing.T) {
	installPkg(t, nil)
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("app.calendly", func() workflow.NodeExecutor { return nil }) // already taken: left alone
	Register(reg)
	for _, typ := range []string{"app.todoist", "app.typeform", "app.calendly"} {
		if !reg.Has(typ) {
			t.Errorf("%s not registered", typ)
		}
	}
	if f, _ := reg.Get("app.todoist"); f().Type() != "app.todoist" {
		t.Error("factory type")
	}

	raw, ok := workflow.ReadEmbeddedSchema("app.todoist")
	if !ok {
		t.Fatal("no schema for app.todoist through the workflow provider")
	}
	if workflow.SchemaTitle("app.todoist") != "Todoist" {
		t.Error("schema title")
	}
	var doc struct {
		CredentialPlatform string           `json:"credential_platform"`
		Fields             []map[string]any `json:"fields"`
	}
	_ = json.Unmarshal(raw, &doc)
	if doc.CredentialPlatform != "todoist" {
		t.Errorf("credential_platform: %s", raw[:200])
	}
	byKey := map[string][]map[string]any{}
	for _, f := range doc.Fields {
		byKey[f["key"].(string)] = append(byKey[f["key"].(string)], f)
	}
	if byKey["credential_id"][0]["type"] != "credential_picker" || byKey["resource"][0]["default"] != "task" {
		t.Errorf("selectors: %v %v", byKey["credential_id"], byKey["resource"])
	}
	if len(byKey["operation"]) != 5 || byKey["operation"][0]["default"] != "create" {
		t.Errorf("one operation select per resource: %v", byKey["operation"])
	}
	content := byKey["content"][0]
	if content["type"] != "textarea" || content["required"] != true || content["depends_on"].(map[string]any)["key"] != "operation" {
		t.Errorf("content field: %v", content)
	}
	if vf := content["visible_for"].([]any)[0].(map[string]any); vf["resource"] != "task" || vf["operation"] != "create" {
		t.Errorf("visible_for: %v", content["visible_for"])
	}
	opts := byKey["options"]
	if len(opts) != 3 || opts[0]["type"] != "code" || opts[0]["language"] != "json" || !strings.Contains(opts[0]["help"].(string), "Keys: description") {
		t.Errorf("collections: %v", opts)
	}
	// taskId (same key/type/label in 7 ops) is one merged field.
	if len(byKey["taskId"]) != 1 || len(byKey["taskId"][0]["visible_for"].([]any)) != 6 {
		t.Errorf("taskId merge: %v", byKey["taskId"])
	}
	s, err := workflow.LoadDefaultSchema("app.todoist")
	if err != nil || s.CredentialPlatform == nil || *s.CredentialPlatform != "todoist" || len(s.Fields) < 10 {
		t.Errorf("LoadDefaultSchema: %v %v", s, err)
	}
	if _, ok := SchemaFor("app.nope"); ok {
		t.Error("SchemaFor unknown type")
	}
}

func TestPlatformsRegisterIntoConnections(t *testing.T) {
	installPkg(t, nil)
	p, ok := connections.Get("todoist")
	if !ok {
		t.Fatal("todoist platform not registered")
	}
	if p.Category != "app" || p.OAuth == nil || p.OAuth.ExtraParams["scope"] != "data:read_write,data:delete" ||
		len(p.Methods) != 2 || p.Fields[connections.MethodAPIKey][0].Key != "access_token" || !p.Fields[connections.MethodAPIKey][0].Secret {
		t.Errorf("todoist platform: %+v", p)
	}
	found := 0
	for _, x := range connections.All() {
		if x.ID == "typeform" || x.ID == "calendly" {
			found++
		}
	}
	if found != 2 {
		t.Error("connections.All() misses package platforms")
	}
	if _, ok := connections.Get("github"); !ok {
		t.Error("built-ins still resolve")
	}
}

func TestInstalledPackagesOverrideAndErrors(t *testing.T) {
	installPkg(t, map[string]string{
		"todoist": strings.Replace(pkgJSON(`{"resource":"","operation":"ping","title":"Ping","method":"GET","path":"/p"}`), `"id":"demo"`, `"id":"todoist"`, 1),
		"broken":  `{"$schema":"apipkg.v1","id":"broken"}`,
		"dupe":    strings.Replace(pkgJSON(`{"resource":"","operation":"ping","title":"Ping","method":"GET","path":"/p"}`), `"id":"demo"`, `"id":"dupe","node_type":"app.typeform"`, 1),
	})
	pkgs, err := Load()
	if err == nil || !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "both claim app.typeform") {
		t.Errorf("errors: %v", err)
	}
	p, _ := ByID("todoist")
	if len(p.Operations) != 1 || !strings.HasSuffix(p.Source, "package.json") {
		t.Errorf("an installed package replaces the catalog one: %s %d", p.Source, len(p.Operations))
	}
	if len(pkgs) != 3 { // todoist (installed), typeform, calendly; dupe and broken left out
		t.Errorf("packages: %d", len(pkgs))
	}
	if !strings.HasSuffix(InstallDir(), ".monoagent/apipkgs") {
		t.Error(InstallDir())
	}
}

func TestN8nIndex(t *testing.T) {
	installPkg(t, nil)
	idx := N8nIndex()
	m, ok := idx["n8n-nodes-base.todoist"]
	if !ok || !m.Supported || m.NodeType != "app.todoist" || m.DefaultResource != "task" || m.DefaultOperations["project"] != "create" {
		t.Fatalf("todoist: %+v", m)
	}
	if op := m.Operations["task.create"]; op.Resource != "task" || !contains(op.Params, "content") {
		t.Errorf("task.create: %+v", op)
	}
	if len(m.Credentials) != 2 || m.Credentials[1].N8nType != "todoistOAuth2Api" || m.Credentials[0].FieldMap["apiKey"] != "access_token" {
		t.Errorf("credentials: %+v", m.Credentials)
	}
	for _, trig := range []string{"n8n-nodes-base.typeformTrigger", "n8n-nodes-base.calendlyTrigger"} {
		if tm := idx[trig]; !tm.Trigger || tm.Supported || tm.PackageID == "" {
			t.Errorf("%s: %+v", trig, tm)
		}
	}
}
