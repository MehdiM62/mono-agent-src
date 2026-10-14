package datatable_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/datatable"
	"github.com/monoes/mono-agent/internal/testdb"
)

func newTable(t *testing.T) (*datatable.Store, datatable.Table) {
	t.Helper()
	return newTableIn(t, testdb.Open(t).DB)
}

func newTableIn(t *testing.T, db *sql.DB) (*datatable.Store, datatable.Table) {
	t.Helper()
	s := datatable.New(db, "p1")
	tbl, err := s.CreateTable(context.Background(), "Leads", []datatable.Column{
		{Name: "name"}, {Name: "score", Type: "number"}, {Name: "active", Type: "boolean"}, {Name: "seen", Type: "date"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, tbl
}

func seed(t *testing.T, s *datatable.Store, tbl *datatable.Table) {
	t.Helper()
	_, err := s.Insert(context.Background(), tbl, []map[string]interface{}{
		{"name": "Ada", "score": 9.0, "active": true, "seen": "2026-01-02"},
		{"name": "bob", "score": "4", "active": "false"},
		{"name": "", "score": 7.0},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func names(rows []map[string]interface{}) string {
	var out []string
	for _, r := range rows {
		out = append(out, r["name"].(string))
	}
	return strings.Join(out, ",")
}

func TestTablesLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	s, tbl := newTableIn(t, db)
	if _, err := s.CreateTable(ctx, "Leads", nil); err == nil {
		t.Error("a second table with the same name was created")
	}
	for _, ref := range []string{tbl.ID, "Leads", "leads"} {
		if got, err := s.FindTable(ctx, ref); err != nil || got.ID != tbl.ID {
			t.Errorf("find %q: %v %v", ref, got, err)
		}
	}
	// Tables belong to their profile.
	if _, err := datatable.New(db, "p2").FindTable(ctx, "Leads"); !errors.Is(err, datatable.ErrNotFound) {
		t.Errorf("other profile: %v", err)
	}
	renamed, err := s.RenameTable(ctx, tbl, "Prospects")
	if err != nil || renamed.Name != "Prospects" {
		t.Fatalf("rename: %v %v", renamed, err)
	}
	seed(t, s, &renamed)
	if n, _ := s.ClearTable(ctx, renamed); n != 3 {
		t.Errorf("cleared %d rows", n)
	}
	if err := s.DeleteTable(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListTables(ctx); len(list) != 0 {
		t.Errorf("tables left: %v", list)
	}
	if _, err := s.CreateTable(ctx, "x", []datatable.Column{{Name: "id"}}); err == nil {
		t.Error("a system column was accepted")
	}
}

func TestInsertCoercesAndAddsColumns(t *testing.T) {
	ctx := context.Background()
	s, tbl := newTable(t)
	rows, err := s.Insert(ctx, &tbl, []map[string]interface{}{{"name": 12.0, "score": "3.5", "seen": "2026-03-04T05:06:07Z", "extra": true, "id": 99.0}})
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if r["name"] != "12" || r["score"] != 3.5 || r["seen"] != "2026-03-04T05:06:07.000Z" || r["active"] != nil || r["extra"] != true {
		t.Errorf("row %v", r)
	}
	if id, _ := r["id"].(int64); id == 99 || id == 0 {
		t.Errorf("id %v", r["id"])
	}
	if typ, ok := tbl.ColumnType("extra"); !ok || typ != "boolean" {
		t.Errorf("extra column %q %v", typ, ok)
	}
	stored, _ := s.FindTable(ctx, tbl.ID)
	if typ, _ := stored.ColumnType("extra"); typ != "boolean" {
		t.Errorf("extra column not saved: %v", stored.Columns)
	}
	if _, err := s.Insert(ctx, &tbl, []map[string]interface{}{{"score": "many"}}); err == nil || !strings.Contains(err.Error(), "score") {
		t.Errorf("bad number: %v", err)
	}
}

func TestSelectFilters(t *testing.T) {
	ctx := context.Background()
	s, tbl := newTable(t)
	seed(t, s, &tbl)
	cases := []struct {
		f    datatable.Filter
		want string
	}{
		{datatable.Filter{}, "Ada,bob,"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "score", Op: "gt", Value: "5"}}}, "Ada,"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "score", Op: "lte", Value: 7.0}}}, "bob,"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Value: "Ada"}}}, "Ada"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Op: "neq", Value: "Ada"}}}, "bob,"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Op: "like", Value: "A%"}}}, "Ada"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Op: "like", Value: "B"}}}, ""},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Op: "ilike", Value: "B"}}}, "bob"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Op: "isEmpty"}}}, ""},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "seen", Op: "isNotEmpty"}}}, "Ada"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "active", Op: "isFalse"}}}, "bob"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "seen", Op: "gte", Value: "2026-01-01"}}}, "Ada"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Value: "Ada"}, {Column: "name", Value: "bob"}}}, "Ada,bob"},
		{datatable.Filter{All: true, Conditions: []datatable.Condition{{Column: "score", Op: "gt", Value: 5.0}, {Column: "active", Op: "isTrue"}}}, "Ada"},
		{datatable.Filter{Conditions: []datatable.Condition{{Column: "id", Op: "gt", Value: 0.0}}}, "Ada,bob,"},
	}
	for i, tc := range cases {
		rows, err := s.Select(ctx, tbl, datatable.Query{Filter: tc.f})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if got := names(rows); got != tc.want {
			t.Errorf("case %d: %q, want %q", i, got, tc.want)
		}
	}
	rows, _ := s.Select(ctx, tbl, datatable.Query{SortBy: "score", Desc: true, Limit: 2})
	if names(rows) != "Ada," {
		t.Errorf("sorted: %q", names(rows))
	}
	if _, err := s.Select(ctx, tbl, datatable.Query{Filter: datatable.Filter{Conditions: []datatable.Condition{{Column: "nope", Value: 1.0}}}}); err == nil {
		t.Error("unknown column accepted")
	}
}

func TestUpdateUpsertDelete(t *testing.T) {
	ctx := context.Background()
	s, tbl := newTable(t)
	seed(t, s, &tbl)
	byName := func(n string) datatable.Filter {
		return datatable.Filter{Conditions: []datatable.Condition{{Column: "name", Value: n}}}
	}
	dry, err := s.Update(ctx, &tbl, byName("bob"), map[string]interface{}{"score": 5.0}, true)
	if err != nil || len(dry) != 2 || dry[0]["score"] != 4.0 || dry[1]["score"] != 5.0 || dry[1][datatable.DryRunState] != "after" {
		t.Fatalf("dry run: %v %v", dry, err)
	}
	if rows, _ := s.Select(ctx, tbl, datatable.Query{Filter: byName("bob")}); rows[0]["score"] != 4.0 {
		t.Errorf("dry run wrote: %v", rows)
	}
	up, err := s.Update(ctx, &tbl, byName("bob"), map[string]interface{}{"score": 5.0, "note": "x"}, false)
	if err != nil || len(up) != 1 || up[0]["score"] != 5.0 || up[0]["name"] != "bob" || up[0]["note"] != "x" {
		t.Fatalf("update: %v %v", up, err)
	}
	ins, err := s.Upsert(ctx, &tbl, byName("cy"), map[string]interface{}{"name": "cy", "score": 1.0}, false)
	if err != nil || len(ins) != 1 || ins[0]["name"] != "cy" {
		t.Fatalf("upsert insert: %v %v", ins, err)
	}
	upd, err := s.Upsert(ctx, &tbl, byName("cy"), map[string]interface{}{"score": 2.0}, false)
	if err != nil || len(upd) != 1 || upd[0]["score"] != 2.0 || upd[0]["id"] != ins[0]["id"] {
		t.Fatalf("upsert update: %v %v", upd, err)
	}
	del, err := s.Delete(ctx, tbl, datatable.Filter{Conditions: []datatable.Condition{{Column: "score", Op: "lt", Value: 6.0}}}, false)
	if err != nil || len(del) != 2 {
		t.Fatalf("delete: %v %v", del, err)
	}
	if n, _ := s.CountRows(ctx, tbl); n != 2 {
		t.Errorf("%d rows left", n)
	}
}
