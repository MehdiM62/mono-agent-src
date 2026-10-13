package dbnodes

import (
	"reflect"
	"testing"
)

func assertStmt(t *testing.T, name string, got sqlStmt, err error, wantQ string, wantArgs ...interface{}) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	if got.Query != wantQ {
		t.Errorf("%s:\n got  %s\n want %s", name, got.Query, wantQ)
	}
	if len(wantArgs) == 0 && len(got.Args) == 0 {
		return
	}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Errorf("%s: args = %#v, want %#v", name, got.Args, wantArgs)
	}
}

func TestBuildInsertPerDialect(t *testing.T) {
	row := map[string]interface{}{"name": "Ada", "age": 36}
	cases := []struct {
		d    *sqlDialect
		want string
	}{
		{postgresDialect, `INSERT INTO "public"."users" ("age", "name") VALUES ($1, $2) RETURNING *`},
		{crateDialect, `INSERT INTO "public"."users" ("age", "name") VALUES ($1, $2) RETURNING *`},
		{questDialect, `INSERT INTO "public"."users" ("age", "name") VALUES ($1, $2)`},
		{mysqlDialect, "INSERT INTO `public`.`users` (`age`, `name`) VALUES (?, ?)"},
		{mssqlDialect, `INSERT INTO [public].[users] ([age], [name]) VALUES (@p1, @p2)`},
		{oracleDialect, `INSERT INTO public.users (age, name) VALUES (:1, :2)`},
		{snowflakeDialect, `INSERT INTO public.users (age, name) VALUES (?, ?)`},
	}
	for _, c := range cases {
		st, err := buildInsert(c.d, writeSpec{Schema: "public", Table: "users", Row: row})
		assertStmt(t, c.d.name, st, err, c.want, 36, "Ada")
		if st.Rows != c.d.returning {
			t.Errorf("%s: Rows = %v, want %v", c.d.name, st.Rows, c.d.returning)
		}
	}
}

func TestBuildInsertSkipOnConflictAndReturning(t *testing.T) {
	row := map[string]interface{}{"id": 1}
	st, err := buildInsert(postgresDialect, writeSpec{Table: "t", Row: row, SkipOnConflict: true, Returning: []string{"id"}})
	assertStmt(t, "pg", st, err, `INSERT INTO "t" ("id") VALUES ($1) ON CONFLICT DO NOTHING RETURNING "id"`, 1)
	st, err = buildInsert(mysqlDialect, writeSpec{Table: "t", Row: row, SkipOnConflict: true})
	assertStmt(t, "mysql", st, err, "INSERT IGNORE INTO `t` (`id`) VALUES (?)", 1)
}

func TestBuildUpdatePerDialect(t *testing.T) {
	w := writeSpec{Table: "users", Row: map[string]interface{}{"id": 7, "name": "Ada", "email": "a@x"}, MatchColumns: []string{"id"}}
	cases := []struct {
		d    *sqlDialect
		want string
	}{
		{postgresDialect, `UPDATE "users" SET "email" = $1, "name" = $2 WHERE "id" = $3 RETURNING *`},
		{mysqlDialect, "UPDATE `users` SET `email` = ?, `name` = ? WHERE `id` = ?"},
		{mssqlDialect, `UPDATE [users] SET [email] = @p1, [name] = @p2 WHERE [id] = @p3`},
		{oracleDialect, `UPDATE users SET email = :1, name = :2 WHERE id = :3`},
	}
	for _, c := range cases {
		st, err := buildUpdate(c.d, w)
		assertStmt(t, c.d.name, st, err, c.want, "a@x", "Ada", 7)
	}
	if _, err := buildUpdate(postgresDialect, writeSpec{Table: "t", Row: map[string]interface{}{"a": 1}}); err == nil {
		t.Error("update without match_columns: want error")
	}
	if _, err := buildUpdate(postgresDialect, writeSpec{Table: "t", Row: map[string]interface{}{"a": 1}, MatchColumns: []string{"id"}}); err == nil {
		t.Error("update with match column missing from row: want error")
	}
}

func TestBuildUpsertPerDialect(t *testing.T) {
	w := writeSpec{Table: "users", Row: map[string]interface{}{"id": 7, "name": "Ada"}, MatchColumns: []string{"id"}}
	cases := []struct {
		d    *sqlDialect
		want string
	}{
		{postgresDialect, `INSERT INTO "users" ("id", "name") VALUES ($1, $2) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name" RETURNING *`},
		{mysqlDialect, "INSERT INTO `users` (`id`, `name`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)"},
		{mssqlDialect, `MERGE INTO [users] AS tgt USING (SELECT @p1 AS [id], @p2 AS [name]) AS src ON (tgt.[id] = src.[id]) WHEN MATCHED THEN UPDATE SET tgt.[name] = src.[name] WHEN NOT MATCHED THEN INSERT ([id], [name]) VALUES (src.[id], src.[name]);`},
		{oracleDialect, `MERGE INTO users tgt USING (SELECT :1 AS id, :2 AS name FROM dual) src ON (tgt.id = src.id) WHEN MATCHED THEN UPDATE SET tgt.name = src.name WHEN NOT MATCHED THEN INSERT (id, name) VALUES (src.id, src.name)`},
		{snowflakeDialect, `MERGE INTO users AS tgt USING (SELECT ? AS id, ? AS name) AS src ON (tgt.id = src.id) WHEN MATCHED THEN UPDATE SET tgt.name = src.name WHEN NOT MATCHED THEN INSERT (id, name) VALUES (src.id, src.name)`},
	}
	for _, c := range cases {
		st, err := buildUpsert(c.d, w)
		assertStmt(t, c.d.name, st, err, c.want, 7, "Ada")
	}
	if _, err := buildUpsert(questDialect, w); err == nil {
		t.Error("questdb upsert: want unsupported error")
	}
	// Only key columns: nothing to update.
	st, err := buildUpsert(postgresDialect, writeSpec{Table: "t", Row: map[string]interface{}{"id": 1}, MatchColumns: []string{"id"}})
	assertStmt(t, "pg-key-only", st, err, `INSERT INTO "t" ("id") VALUES ($1) ON CONFLICT ("id") DO NOTHING RETURNING *`, 1)
}

func TestBuildSelectWithConditionsSortLimit(t *testing.T) {
	spec := selectSpec{
		Table:   "orders",
		Columns: []string{"id", "total"},
		Conditions: []sqlCondition{
			{Column: "status", Op: "equal", Value: "open"},
			{Column: "total", Op: ">=", Value: 10},
			{Column: "deleted_at", Op: "IS NULL"},
		},
		Combine: "AND",
		Sort:    []sqlSort{{Column: "total", Direction: "desc"}, {Column: "id"}},
		Limit:   5,
	}
	cases := []struct {
		d    *sqlDialect
		want string
	}{
		{postgresDialect, `SELECT "id", "total" FROM "orders" WHERE "status" = $1 AND "total" >= $2 AND "deleted_at" IS NULL ORDER BY "total" DESC, "id" ASC LIMIT 5`},
		{mysqlDialect, "SELECT `id`, `total` FROM `orders` WHERE `status` = ? AND `total` >= ? AND `deleted_at` IS NULL ORDER BY `total` DESC, `id` ASC LIMIT 5"},
		{mssqlDialect, `SELECT TOP (5) [id], [total] FROM [orders] WHERE [status] = @p1 AND [total] >= @p2 AND [deleted_at] IS NULL ORDER BY [total] DESC, [id] ASC`},
		{oracleDialect, `SELECT id, total FROM orders WHERE status = :1 AND total >= :2 AND deleted_at IS NULL ORDER BY total DESC, id ASC FETCH FIRST 5 ROWS ONLY`},
	}
	for _, c := range cases {
		st, err := buildSelect(c.d, spec)
		assertStmt(t, c.d.name, st, err, c.want, "open", 10)
	}
}

func TestBuildSelectOrAndRawWhere(t *testing.T) {
	st, err := buildSelect(postgresDialect, selectSpec{
		Table: "t", RawWhere: "tenant = $1", RawArgs: []interface{}{"acme"},
		Conditions: []sqlCondition{{Column: "a", Op: "LIKE", Value: "x%"}, {Column: "b", Op: "!=", Value: 2}},
		Combine:    "OR",
	})
	assertStmt(t, "pg", st, err, `SELECT * FROM "t" WHERE (tenant = $1) AND ("a" LIKE $2 OR "b" <> $3)`, "acme", "x%", 2)
	if _, err := buildSelect(postgresDialect, selectSpec{Table: "t", RawWhere: "1=1; DROP TABLE t"}); err == nil {
		t.Error("stacked raw where: want error")
	}
	if _, err := buildSelect(postgresDialect, selectSpec{Table: "t", Conditions: []sqlCondition{{Column: "a", Op: "BETWEEN"}}}); err == nil {
		t.Error("unknown operator: want error")
	}
}

func TestIdentifierQuotingEscapes(t *testing.T) {
	st, err := buildSelect(postgresDialect, selectSpec{Table: `we"ird`, Columns: []string{"first name"}})
	assertStmt(t, "pg", st, err, `SELECT "first name" FROM "we""ird"`)
	st, err = buildSelect(mssqlDialect, selectSpec{Table: "a]b"})
	assertStmt(t, "mssql", st, err, `SELECT * FROM [a]]b]`)
	st, err = buildSelect(mysqlDialect, selectSpec{Table: "a`b"})
	assertStmt(t, "mysql", st, err, "SELECT * FROM `a``b`")
	st, err = buildSelect(oracleDialect, selectSpec{Table: "Mixed Case"})
	assertStmt(t, "oracle", st, err, `SELECT * FROM "Mixed Case"`)
	if _, err := buildSelect(postgresDialect, selectSpec{Table: ""}); err == nil {
		t.Error("empty table: want error")
	}
}

func TestBuildDeleteTable(t *testing.T) {
	cases := []struct {
		d                *sqlDialect
		cmd              string
		cascade, restart bool
		want             string
	}{
		{postgresDialect, "truncate", true, true, `TRUNCATE TABLE "t" RESTART IDENTITY CASCADE`},
		{postgresDialect, "drop", true, false, `DROP TABLE IF EXISTS "t" CASCADE`},
		{mysqlDialect, "truncate", true, true, "TRUNCATE TABLE `t`"},
		{oracleDialect, "drop", true, false, `DROP TABLE t CASCADE CONSTRAINTS`},
		{postgresDialect, "delete", false, false, `DELETE FROM "t"`},
	}
	for _, c := range cases {
		st, err := buildDeleteTable(c.d, "", "t", c.cmd, nil, "", c.cascade, c.restart)
		assertStmt(t, c.d.name+"/"+c.cmd, st, err, c.want)
	}
	st, err := buildDeleteTable(mysqlDialect, "", "t", "delete", []sqlCondition{{Column: "id", Value: 3}}, "", false, false)
	assertStmt(t, "mysql delete", st, err, "DELETE FROM `t` WHERE `id` = ?", 3)
	if _, err := buildDeleteTable(postgresDialect, "", "t", "nuke", nil, "", false, false); err == nil {
		t.Error("unknown command: want error")
	}
}

func TestRewriteDollarParams(t *testing.T) {
	q, args, err := rewriteDollarParams(mysqlDialect, "SELECT * FROM t WHERE a = $2 AND b = $1 AND c = '$1' AND d = $2", []interface{}{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if q != "SELECT * FROM t WHERE a = ? AND b = ? AND c = '$1' AND d = ?" {
		t.Errorf("mysql query = %q", q)
	}
	if !reflect.DeepEqual(args, []interface{}{"y", "x", "y"}) {
		t.Errorf("mysql args = %v", args)
	}
	q, args, err = rewriteDollarParams(mssqlDialect, "SELECT $1, $2", []interface{}{1, 2})
	if err != nil || q != "SELECT @p1, @p2" || len(args) != 2 {
		t.Errorf("mssql = %q %v %v", q, args, err)
	}
	q, _, _ = rewriteDollarParams(postgresDialect, "SELECT $1", []interface{}{1})
	if q != "SELECT $1" {
		t.Errorf("postgres must stay native, got %q", q)
	}
	if _, _, err := rewriteDollarParams(mysqlDialect, "SELECT $3", []interface{}{1}); err == nil {
		t.Error("out-of-range $3: want error")
	}
}
