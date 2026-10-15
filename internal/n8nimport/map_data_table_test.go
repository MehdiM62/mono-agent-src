package n8nimport_test

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/nodes/control"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

const dataTableWorkflow = `{"name":"dt","nodes":[
 {"name":"Create","type":"n8n-nodes-base.dataTable","typeVersion":1.1,"position":[0,0],"parameters":{
  "resource":"table","operation":"create","tableName":"Leads",
  "columns":{"column":[{"name":"email","type":"string"},{"name":"score","type":"number"}]}}},
 {"name":"Save","type":"n8n-nodes-base.dataTable","typeVersion":1.1,"position":[200,0],"parameters":{
  "dataTableId":{"__rl":true,"mode":"list","value":"x7Kq","cachedResultName":"Leads"},
  "columns":{"mappingMode":"defineBelow","value":{"email":"={{ $json.mail }}","score":"={{ $json.points }}"},"schema":[]}}},
 {"name":"Find","type":"n8n-nodes-base.dataTable","typeVersion":1.1,"position":[400,0],"parameters":{
  "operation":"get","dataTableId":{"__rl":true,"mode":"name","value":"Leads"},"matchType":"allConditions",
  "filters":{"conditions":[{"keyName":"score","condition":"gt","keyValue":"={{ $json.min }}"}]},
  "orderBy":true,"orderByColumn":"score","orderByDirection":"ASC"}},
 {"name":"Known","type":"n8n-nodes-base.dataTable","typeVersion":1.1,"position":[600,0],"parameters":{
  "operation":"rowExists","dataTableId":{"__rl":true,"mode":"name","value":"Leads"},
  "filters":{"conditions":[{"keyName":"email","keyValue":"={{ $json.mail }}"}]}}}
],"connections":{}}`

// n8n Data table nodes import as core.data_table and run against the
// profile's tables: create, insert with per-item values, filtered get and
// rowExists.
func TestDataTableImportsAndRuns(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(dataTableWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Create", "Save", "Find", "Known"} {
		if n := node(t, wf, name); n.Type != "core.data_table" {
			t.Fatalf("%s: type %s\n%s", name, n.Type, issueText(rep, name))
		}
		if nr := nodeReport(t, rep, name); nr.Status != n8nimport.StatusFull {
			t.Errorf("%s: status %s\n%s", name, nr.Status, issueText(rep, name))
		}
	}
	if save := node(t, wf, "Save"); save.Config["table"] != "Leads" {
		t.Errorf("table by cached name: %v", save.Config)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), testdb.Open(t).DB), "default")
	run := func(name string, items ...map[string]interface{}) []map[string]interface{} {
		t.Helper()
		in := workflow.NodeInput{}
		for _, j := range items {
			in.Items = append(in.Items, workflow.Item{JSON: j})
		}
		out, err := (&control.DataTableNode{}).Execute(ctx, in, node(t, wf, name).Config)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var res []map[string]interface{}
		for _, it := range out[0].Items {
			res = append(res, it.JSON)
		}
		return res
	}
	run("Create")
	run("Save", map[string]interface{}{"mail": "a@x", "points": 4.0}, map[string]interface{}{"mail": "b@x", "points": 9.0})
	found := run("Find", map[string]interface{}{"min": 3.0})
	if len(found) != 2 || found[0]["email"] != "a@x" || found[1]["score"] != 9.0 {
		t.Errorf("find: %v", found)
	}
	if known := run("Known", map[string]interface{}{"mail": "b@x"}, map[string]interface{}{"mail": "c@x"}); len(known) != 1 || known[0]["mail"] != "b@x" {
		t.Errorf("rowExists: %v", known)
	}
}
