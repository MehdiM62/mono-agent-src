package n8napps

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/workflow"
)

func convertOne(t *testing.T, n8nType string, params map[string]interface{}) (*workflow.WorkflowFile, *n8nimport.Report) {
	t.Helper()
	p, _ := json.Marshal(params)
	src := fmt.Sprintf(`{"name":"t","nodes":[{"name":"T","type":%q,"typeVersion":1,"position":[0,0],"webhookId":"hook-1","parameters":%s}],"connections":{}}`, n8nType, p)
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatalf("%s: %v", n8nType, err)
	}
	return wf, rep
}

// Every n8n trigger a catalog package lists is handled by the trigger
// tables: none falls back to the generic "not supported yet" placeholder.
func TestEveryCatalogTriggerIsMapped(t *testing.T) {
	apipkg.Reload()
	var types []string
	for typ, m := range apipkg.N8nIndex() {
		if m.Trigger {
			types = append(types, typ)
		}
	}
	sort.Strings(types)
	if len(types) < 70 {
		t.Fatalf("only %d trigger types in the catalog", len(types))
	}
	for _, typ := range types {
		_, rep := convertOne(t, typ, map[string]interface{}{})
		if strings.Contains(rep.Text(), "is not supported yet") {
			t.Errorf("%s has no trigger mapping:\n%s", typ, rep.Text())
		}
	}
}

// Each polling trigger's list step names an operation of its package and
// only that operation's params; its templates resolve.
func TestPollingTriggersUsePackageOperations(t *testing.T) {
	apipkg.Reload()
	idx := apipkg.N8nIndex()
	ops := map[string]map[string]apipkg.N8nOperation{}
	for _, m := range idx {
		if m.Supported {
			ops[m.PackageID] = m.Operations
		}
	}
	rl := func(v string) map[string]interface{} { return map[string]interface{}{"__rl": true, "mode": "id", "value": v} }
	cases := map[string]map[string]interface{}{
		"gmailTrigger":                 {"filters": map[string]interface{}{"q": "invoice"}},
		"googleSheetsTrigger":          {"documentId": rl("doc"), "sheetName": rl("Sheet1"), "options": map[string]interface{}{"valueRender": "FORMATTED_VALUE"}},
		"airtableTrigger":              {"baseId": rl("app1"), "tableId": rl("tbl1"), "triggerField": "Modified", "additionalFields": map[string]interface{}{"fields": "Name", "formula": "{Done}"}},
		"googleCalendarTrigger":        {"calendarId": rl("primary"), "triggerOn": "eventStarted"},
		"googleDriveTrigger":           {"triggerOn": "specificFolder", "folderToWatch": rl("fold1"), "event": "fileUpdated"},
		"notionTrigger":                {"databaseId": rl("db1"), "event": "pagedUpdatedInDatabase"},
		"microsoftOutlookTrigger":      {"filters": map[string]interface{}{"readStatus": "unread"}},
		"salesforceTrigger":            {"triggerOn": "leadUpdated"},
		"microsoftSharePointTrigger":   {"resource": "list", "site": rl("s1"), "list": rl("l1")},
		"microsoftOneDriveTrigger":     {"watchFolder": true, "folderId": rl("f1"), "event": "fileUpdated"},
		"googleBusinessProfileTrigger": {"account": rl("accounts/1"), "location": rl("locations/2")},
		"seaTableTrigger":              {"tableName": "Tasks", "event": "updatedRow"},
	}
	engine := workflow.NewExpressionEngine()
	for name, params := range cases {
		typ := "n8n-nodes-base." + name
		wf, rep := convertOne(t, typ, params)
		var list *workflow.WorkflowFileNode
		for i := range wf.Nodes {
			if strings.HasSuffix(wf.Nodes[i].Name, " (list)") {
				list = &wf.Nodes[i]
			}
		}
		if list == nil {
			t.Errorf("%s: no list step:\n%s", name, rep.Text())
			continue
		}
		key := fmt.Sprint(list.Config["resource"], ".", list.Config["operation"])
		op, ok := ops[idx[typ].PackageID][key]
		if !ok {
			t.Errorf("%s: package %s has no operation %s", name, idx[typ].PackageID, key)
			continue
		}
		for k := range list.Config {
			if k != "resource" && k != "operation" && k != "credential_id" && !contains(op.Params, k) {
				t.Errorf("%s: %s is not a param of %s (%v)", name, k, key, op.Params)
			}
		}
		if _, err := engine.ResolveConfig(list.Config, workflow.ExpressionContext{}); err != nil {
			t.Errorf("%s: list step templates: %v", name, err)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
