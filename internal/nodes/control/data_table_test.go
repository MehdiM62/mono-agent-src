package control

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

func dtCtx(t *testing.T) context.Context {
	t.Helper()
	return vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), testdb.Open(t).DB), "p1")
}

func dtRun(t *testing.T, ctx context.Context, items []map[string]interface{}, cfg map[string]interface{}) []map[string]interface{} {
	t.Helper()
	out, err := dtTry(ctx, items, cfg)
	if err != nil {
		t.Fatalf("%v: %v", cfg, err)
	}
	return out
}

func dtTry(ctx context.Context, items []map[string]interface{}, cfg map[string]interface{}) ([]map[string]interface{}, error) {
	in := workflow.NodeInput{}
	for _, j := range items {
		in.Items = append(in.Items, workflow.Item{JSON: j})
	}
	out, err := (&DataTableNode{}).Execute(ctx, in, cfg)
	if err != nil {
		return nil, err
	}
	var res []map[string]interface{}
	for _, it := range out[0].Items {
		res = append(res, it.JSON)
	}
	return res, nil
}

func TestDataTableRowsEndToEnd(t *testing.T) {
	ctx := dtCtx(t)
	created := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "create", "table": "Leads",
		"columns": `[{"name": "email", "type": "string"}, {"name": "score", "type": "number"}]`})
	if created[0]["name"] != "Leads" || created[0]["id"] == "" {
		t.Fatalf("create: %v", created)
	}
	// Reusing the existing table is the default.
	if again := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "create", "table": "Leads"}); again[0]["id"] != created[0]["id"] {
		t.Errorf("reuse: %v", again)
	}
	people := []map[string]interface{}{{"email": "a@x", "score": 3.0}, {"email": "b@x", "score": 8.0}}
	// Values resolved per item, as the engine leaves them for the node.
	ins := dtRun(t, ctx, people, map[string]interface{}{"operation": "insert", "table": "leads",
		"values": map[string]interface{}{"email": "{{ $json.email }}", "score": "{{ $json.score }}"}})
	if len(ins) != 2 || ins[1]["email"] != "b@x" || ins[1]["score"] != 8.0 || ins[1]["id"] == nil {
		t.Fatalf("insert: %v", ins)
	}
	auto := dtRun(t, ctx, []map[string]interface{}{{"email": "c@x", "score": 5.0, "id": 1.0, "city": "Oslo"}},
		map[string]interface{}{"operation": "insert", "table": created[0]["id"], "columns_mode": "auto"})
	if auto[0]["city"] != "Oslo" || auto[0]["id"] == 1.0 {
		t.Errorf("auto insert: %v", auto)
	}
	got := dtRun(t, ctx, nil, map[string]interface{}{"operation": "get", "table": "Leads", "match": "all",
		"filters": `[{"column": "score", "condition": "gte", "value": "4"}]`, "order_by": "score", "order_direction": "ASC"})
	if len(got) != 2 || got[0]["email"] != "c@x" || got[1]["email"] != "b@x" {
		t.Errorf("get: %v", got)
	}
	// rowExists / rowNotExists pass the input items through.
	probe := []map[string]interface{}{{"email": "a@x"}, {"email": "z@x"}}
	filter := []interface{}{map[string]interface{}{"column": "email", "value": "{{ $json.email }}"}}
	if ex := dtRun(t, ctx, probe, map[string]interface{}{"operation": "rowExists", "table": "Leads", "filters": filter}); len(ex) != 1 || ex[0]["email"] != "a@x" {
		t.Errorf("rowExists: %v", ex)
	}
	if nex := dtRun(t, ctx, probe, map[string]interface{}{"operation": "rowNotExists", "table": "Leads", "filters": filter}); len(nex) != 1 || nex[0]["email"] != "z@x" {
		t.Errorf("rowNotExists: %v", nex)
	}
	up := dtRun(t, ctx, []map[string]interface{}{{"email": "z@x", "score": 1.0}}, map[string]interface{}{"operation": "upsert", "table": "Leads",
		"filters": filter, "values": map[string]interface{}{"email": "{{ $json.email }}", "score": "{{ $json.score }}"}})
	if len(up) != 1 || up[0]["email"] != "z@x" {
		t.Errorf("upsert: %v", up)
	}
	upd := dtRun(t, ctx, nil, map[string]interface{}{"operation": "update", "table": "Leads",
		"filters": []interface{}{map[string]interface{}{"column": "email", "condition": "like", "value": "@x"}}, "values": map[string]interface{}{"score": 0.0}})
	if len(upd) != 4 {
		t.Errorf("update: %v", upd)
	}
	if _, err := dtTry(ctx, nil, map[string]interface{}{"operation": "deleteRows", "table": "Leads"}); err == nil || !strings.Contains(err.Error(), "condition") {
		t.Errorf("delete without a condition: %v", err)
	}
	del := dtRun(t, ctx, nil, map[string]interface{}{"operation": "deleteRows", "table": "Leads",
		"filters": []interface{}{map[string]interface{}{"column": "email", "value": "a@x"}}})
	if len(del) != 1 {
		t.Errorf("delete: %v", del)
	}
	if lim := dtRun(t, ctx, nil, map[string]interface{}{"operation": "get", "table": "Leads", "limit": 2}); len(lim) != 2 {
		t.Errorf("limit: %v", lim)
	}
	if _, err := dtTry(ctx, nil, map[string]interface{}{"operation": "get", "table": "Nope"}); err == nil {
		t.Error("missing table: no error")
	}
}

func TestDataTableTableOps(t *testing.T) {
	ctx := dtCtx(t)
	for _, name := range []string{"beta", "Alpha"} {
		dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "create", "table": name})
	}
	list := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "list"})
	if len(list) != 2 || list[0]["name"] != "Alpha" {
		t.Errorf("list: %v", list)
	}
	if f := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "list", "filter_name": "ET"}); len(f) != 1 || f[0]["name"] != "beta" {
		t.Errorf("filtered list: %v", f)
	}
	dtRun(t, ctx, nil, map[string]interface{}{"operation": "insert", "table": "beta", "values": map[string]interface{}{"n": 1.0}})
	if r := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "update", "table": "beta", "new_name": "Gamma"}); r[0]["name"] != "Gamma" {
		t.Errorf("rename: %v", r)
	}
	if c := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "clear", "table": "Gamma"}); c[0]["deletedCount"] != int64(1) {
		t.Errorf("clear: %v", c)
	}
	if d := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "delete", "table": "Gamma"}); d[0]["success"] != true {
		t.Errorf("delete: %v", d)
	}
	if list := dtRun(t, ctx, nil, map[string]interface{}{"resource": "table", "operation": "list"}); len(list) != 1 {
		t.Errorf("after delete: %v", list)
	}
}
