package dbnodes

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/monoes/mono-agent/internal/workflow"
)

// withMockSQL routes openSQL and openMySQL to a sqlmock DB for one test.
func withMockSQL(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	prevSQL, prevMy := openSQL, openMySQL
	openSQL = func(string, string) (*sql.DB, error) { return db, nil }
	openMySQL = func(string) (*sql.DB, error) { return db, nil }
	t.Cleanup(func() {
		openSQL, openMySQL = prevSQL, prevMy
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("sqlmock: %v", err)
		}
	})
	mock.ExpectPing()
	return mock
}

func items(objs ...map[string]interface{}) workflow.NodeInput {
	in := workflow.NodeInput{}
	for _, o := range objs {
		in.Items = append(in.Items, workflow.NewItem(o))
	}
	return in
}

func mainItems(t *testing.T, out []workflow.NodeOutput) []workflow.Item {
	t.Helper()
	for _, o := range out {
		if o.Handle == "main" {
			return o.Items
		}
	}
	return nil
}

func TestIsN8nSQLConfigKeepsLegacyConfigs(t *testing.T) {
	legacy := []map[string]interface{}{
		{"operation": "select", "query": "SELECT 1"},
		{"operation": "select", "table": "t", "where": "id = $1", "params": []interface{}{1}},
		{"operation": "insert", "table": "t", "data": map[string]interface{}{"a": 1}},
		{"operation": "execute", "query": "DELETE FROM t WHERE id = 1"},
		{"operation": "select", "table": "t", "conditions": "", "return_all": false, "limit": float64(0)}, // untouched UI fields
	}
	for i, c := range legacy {
		if isN8nSQLConfig(c) {
			t.Errorf("legacy config %d switched to n8n mode: %v", i, c)
		}
	}
	n8n := []map[string]interface{}{
		{"operation": "executeQuery", "query": "SELECT 1"},
		{"operation": "upsert", "table": "t"},
		{"operation": "deleteTable", "table": "t"},
		{"operation": "insert", "table": "t"},
		{"operation": "select", "table": "t", "limit": float64(10)},
		{"operation": "select", "table": "t", "conditions": []interface{}{map[string]interface{}{"column": "a"}}},
	}
	for i, c := range n8n {
		if !isN8nSQLConfig(c) {
			t.Errorf("n8n config %d not detected: %v", i, c)
		}
	}
}

func TestPostgresLegacySelectUnchanged(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectQuery(`SELECT * FROM "users" WHERE id = $1`).WithArgs(42).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(42, []byte("Ada")))
	out, err := (&PostgresNode{}).Execute(context.Background(), items(map[string]interface{}{"x": 1}, map[string]interface{}{"x": 2}), map[string]interface{}{
		"connection_string": "postgres://x", "operation": "select", "table": "users", "where": "id = $1", "params": []interface{}{42},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mainItems(t, out)
	if len(got) != 1 || got[0].JSON["name"] != "Ada" {
		t.Fatalf("legacy select ran per item or lost data: %+v", got)
	}
}

func TestPostgresAutoMapInsertInTransaction(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	q := `INSERT INTO "public"."users" ("email", "name") VALUES ($1, $2) RETURNING "id"`
	mock.ExpectQuery(q).WithArgs("a@x", "Ada").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(q).WithArgs("b@x", "Bob").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	mock.ExpectCommit()
	out, err := (&PostgresNode{}).Execute(context.Background(),
		items(map[string]interface{}{"name": "Ada", "email": "a@x", "skip": true}, map[string]interface{}{"name": "Bob", "email": "b@x"}),
		map[string]interface{}{
			"connection_string": "postgres://x", "operation": "insert", "schema": "public", "table": "users",
			"columns": "name, email", "output_columns": []interface{}{"id"},
		})
	if err != nil {
		t.Fatal(err)
	}
	got := mainItems(t, out)
	if len(got) != 2 || got[1].JSON["id"] != int64(2) {
		t.Fatalf("got %+v", got)
	}
}

func TestPostgresTransactionRollsBackOnError(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "t" WHERE "id" = $1`).WithArgs(1).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	_, err := (&PostgresNode{}).Execute(context.Background(), items(map[string]interface{}{"id": 1}), map[string]interface{}{
		"connection_string": "postgres://x", "operation": "delete", "table": "t", "match_columns": "id", "query_batching": "transaction",
	})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestPostgresIndependentRoutesErrors(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectQuery(`SELECT * FROM t WHERE id = $1`).WithArgs("1").WillReturnError(errors.New("boom"))
	mock.ExpectQuery(`SELECT * FROM t WHERE id = $1`).WithArgs("2").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	out, err := (&PostgresNode{}).Execute(context.Background(), items(map[string]interface{}{"id": 1}, map[string]interface{}{"id": 2}), map[string]interface{}{
		"connection_string": "postgres://x", "operation": "executeQuery", "query": "SELECT * FROM t WHERE id = $1",
		"query_replacement": "{{ $json.id }}", "query_batching": "independently",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Handle != "error" || out[1].Items[0].JSON["error"] != "boom" {
		t.Fatalf("outputs = %+v", out)
	}
}

func TestMySQLUpsertDefineModeAndExecShape(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `users` (`id`, `name`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)").
		WithArgs(float64(9), "Ada").WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()
	out, err := (&MySQLNode{}).Execute(context.Background(), items(map[string]interface{}{"n": "Ada"}), map[string]interface{}{
		"connection_string": "u:p@tcp(h:3306)/db", "operation": "upsert", "table": "users", "data_mode": "define",
		"values": map[string]interface{}{"name": "{{ $json.n }}"}, "match_columns": []interface{}{"id"}, "value_to_match_on": float64(9),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mainItems(t, out)
	if len(got) != 1 || got[0].JSON["success"] != true || got[0].JSON["rows_affected"] != int64(1) || got[0].JSON["last_insert_id"] != int64(9) {
		t.Fatalf("got %+v", got)
	}
}

func TestMySQLExecuteQueryRewritesDollarParams(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT * FROM t WHERE a = ? AND b = ?").WithArgs("2", "1").
		WillReturnRows(sqlmock.NewRows([]string{"a"}))
	mock.ExpectCommit()
	out, err := (&MySQLNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"connection_string": "u:p@tcp(h:3306)/db", "operation": "executeQuery",
		"query": "SELECT * FROM t WHERE a = $2 AND b = $1", "query_replacement": "1, 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mainItems(t, out); len(got) != 0 {
		t.Fatalf("empty result should yield no items, got %+v", got)
	}
}

func TestMSSQLSelectAndDefaultOperation(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT TOP (2) * FROM [dbo].[orders] WHERE [status] = @p1`).WithArgs("open").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
	mock.ExpectCommit()
	out, err := NewMSSQLNode().Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"connection_string": "sqlserver://x", "operation": "select", "schema": "dbo", "table": "orders", "limit": float64(2),
		"conditions": []interface{}{map[string]interface{}{"column": "status", "condition": "equal", "value": "open"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mainItems(t, out); len(got) != 2 {
		t.Fatalf("got %+v", got)
	}

	// No operation -> executeQuery; a statement without result columns yields {success:true}.
	mock2 := withMockSQL(t)
	mock2.ExpectBegin()
	mock2.ExpectQuery(`EXEC sp_refresh`).WillReturnRows(sqlmock.NewRows(nil))
	mock2.ExpectCommit()
	out, err = NewOracleNode().Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"connection_string": "oracle://x", "query": "EXEC sp_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mainItems(t, out); len(got) != 1 || got[0].JSON["success"] != true {
		t.Fatalf("got %+v", got)
	}
}

func TestPgWireNodesUseTheirDialect(t *testing.T) {
	mock := withMockSQL(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "trades" ("price", "sym") VALUES ($1, $2)`).WithArgs(1.5, "BTC").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err := NewQuestDBNode().Execute(context.Background(), items(map[string]interface{}{"sym": "BTC", "price": 1.5}), map[string]interface{}{
		"connection_string": "postgres://x", "operation": "insert", "table": "trades",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCrateDBNode().Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{}); err == nil {
		t.Error("missing connection_string: want error")
	}
}

func TestBuildRowModes(t *testing.T) {
	item := workflow.NewItem(map[string]interface{}{"id": 1, "name": "", "extra": "x"})
	row, err := buildRow(map[string]interface{}{"columns": []interface{}{"name"}, "match_columns": "id", "replace_empty_strings": true}, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(row) != 2 || row["name"] != nil || row["id"] != 1 {
		t.Errorf("auto_map row = %v", row)
	}
	if _, err := buildRow(map[string]interface{}{"data_mode": "weird"}, item); err == nil {
		t.Error("bad data_mode: want error")
	}
	row, _ = buildRow(map[string]interface{}{"values": `{"a": 1}`}, item)
	if len(row) != 1 || row["a"] != float64(1) {
		t.Errorf("define row from JSON string = %v", row)
	}
}
