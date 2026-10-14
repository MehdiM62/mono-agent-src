package dbnodes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// sqlPerItemFields are the SQL-node config keys that commonly read from $json
// and so are resolved once per input item (see workflow.PerItemConfigResolver).
// Legacy configs resolve them against the first item, which is what the engine
// did for them before they were declared here.
var sqlPerItemFields = []string{"query", "params", "data", "where", "query_replacement", "values", "value_to_match_on", "conditions", "bind_params", "values_to_send"}

// n8nOnlySQLOps are operation names that only exist in the n8n-compatible mode.
var n8nOnlySQLOps = map[string]bool{"executeQuery": true, "upsert": true, "deleteTable": true}

// n8nSQLKeys are config keys that only the n8n-compatible mode reads.
var n8nSQLKeys = []string{
	"n8n_compat", "data_mode", "values", "columns", "match_columns", "value_to_match_on", "conditions",
	"combine_conditions", "sort", "limit", "return_all", "output_columns", "query_replacement",
	"query_params_fields", "query_batching", "schema", "delete_command", "cascade",
	"restart_sequences", "skip_on_conflict", "replace_empty_strings", "bind_params", "values_to_send",
}

// isN8nSQLConfig reports whether a db.postgres / db.mysql config uses the
// n8n-compatible operations. Configs that don't keep the original behaviour
// byte for byte.
func isN8nSQLConfig(config map[string]interface{}) bool {
	op := cfgString(config, "operation")
	if n8nOnlySQLOps[op] {
		return true
	}
	for _, k := range n8nSQLKeys {
		if !isZeroConfigValue(config[k]) {
			return true
		}
	}
	// insert/update with no row data and no raw query used to fail; n8n maps
	// the input item's fields to columns instead.
	if (op == "insert" || op == "update") && isZeroConfigValue(config["data"]) && cfgString(config, "query") == "" {
		return true
	}
	return false
}

// isZeroConfigValue reports whether v is absent or a zero value ("", false,
// 0, empty list/object) — the UI may save untouched fields that way, and
// those must not switch a legacy config into the n8n-compatible mode.
func isZeroConfigValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case bool:
		return !t
	case float64:
		return t == 0
	case int:
		return t == 0
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

// sqlResult is what running one statement produced.
type sqlResult struct {
	Columns  []string
	Rows     []map[string]interface{}
	Affected int64
	LastID   int64
}

// sqlRunner runs statements; dbRunner adapts *sql.DB / *sql.Tx and the
// Snowflake REST client implements it directly.
type sqlRunner interface {
	run(ctx context.Context, st sqlStmt) (sqlResult, error)
}

// sqlTxRunner is a runner that can open a transaction.
type sqlTxRunner interface {
	sqlRunner
	begin(ctx context.Context) (sqlTx, error)
}

type sqlTx interface {
	sqlRunner
	commit() error
	rollback() error
}

type sqlQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

type dbRunner struct{ q sqlQueryer }

func (r dbRunner) run(ctx context.Context, st sqlStmt) (sqlResult, error) {
	if !st.Rows {
		res, err := r.q.ExecContext(ctx, st.Query, st.Args...)
		if err != nil {
			return sqlResult{}, err
		}
		out := sqlResult{}
		out.Affected, _ = res.RowsAffected()
		out.LastID, _ = res.LastInsertId()
		return out, nil
	}
	rows, err := r.q.QueryContext(ctx, st.Query, st.Args...)
	if err != nil {
		return sqlResult{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return sqlResult{}, fmt.Errorf("columns: %w", err)
	}
	out := sqlResult{Columns: cols}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return sqlResult{}, fmt.Errorf("scan: %w", err)
		}
		row := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			row[c] = normalizeValue(vals[i])
		}
		out.Rows = append(out.Rows, row)
	}
	return out, rows.Err()
}

func (r dbRunner) begin(ctx context.Context) (sqlTx, error) {
	db, ok := r.q.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("transactions are not available")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return dbTx{dbRunner{tx}, tx}, nil
}

type dbTx struct {
	dbRunner
	tx *sql.Tx
}

func (t dbTx) commit() error   { return t.tx.Commit() }
func (t dbTx) rollback() error { return t.tx.Rollback() }

// runSQLOps executes an n8n-compatible SQL operation once per input item.
// query_batching: "single" (default) and "transaction" run every item inside
// one transaction and fail the node on the first error; "independently" runs
// each item on its own and routes failures to the "error" output.
func runSQLOps(ctx context.Context, nodeType string, d *sqlDialect, runner sqlRunner, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	op := cfgString(config, "operation")
	if op == "" {
		op = "executeQuery"
	}
	batching := strings.ToLower(cfgString(config, "query_batching"))
	switch batching {
	case "", "single", "transaction", "multiple":
	case "independently":
	default:
		return nil, fmt.Errorf("%s: 'query_batching' must be single, transaction or independently (got %q)", nodeType, batching)
	}
	independent := batching == "independently"

	target := runner
	var tx sqlTx
	if !independent {
		if txr, ok := runner.(sqlTxRunner); ok {
			var err error
			if tx, err = txr.begin(ctx); err != nil {
				return nil, fmt.Errorf("%s: begin transaction: %w", nodeType, err)
			}
			target = tx
		}
	}

	var mainItems, errItems []workflow.Item
	for i, item := range inputItems(input) {
		out, err := runSQLItem(ctx, d, target, op, config, item, input)
		if err != nil {
			if independent {
				errItems = append(errItems, errorItem(item, err))
				continue
			}
			if tx != nil {
				_ = tx.rollback()
			}
			return nil, fmt.Errorf("%s: %s (item %d): %w", nodeType, op, i, err)
		}
		mainItems = append(mainItems, out...)
	}
	if tx != nil {
		if err := tx.commit(); err != nil {
			return nil, fmt.Errorf("%s: commit: %w", nodeType, err)
		}
	}
	return mainOutputs(mainItems, errItems), nil
}

func runSQLItem(ctx context.Context, d *sqlDialect, r sqlRunner, op string, base map[string]interface{}, item workflow.Item, input workflow.NodeInput) ([]workflow.Item, error) {
	cfg, err := resolveForItem(ctx, base, sqlPerItemFields, item, input)
	if err != nil {
		return nil, err
	}
	st, err := planSQL(d, op, cfg, item)
	if err != nil {
		return nil, err
	}
	res, err := r.run(ctx, st)
	if err != nil {
		return nil, err
	}
	if st.Rows && len(res.Columns) > 0 {
		items := make([]workflow.Item, 0, len(res.Rows))
		for _, row := range res.Rows {
			items = append(items, workflow.NewItem(row))
		}
		return items, nil
	}
	out := map[string]interface{}{"success": true}
	if !st.Rows {
		out["rows_affected"] = res.Affected
		if res.LastID > 0 {
			out["last_insert_id"] = res.LastID
		}
	}
	return []workflow.Item{workflow.NewItem(out)}, nil
}

// planSQL turns one item's resolved config into the statement to run.
func planSQL(d *sqlDialect, op string, cfg map[string]interface{}, item workflow.Item) (sqlStmt, error) {
	schema, table := cfgString(cfg, "schema"), cfgString(cfg, "table")
	switch op {
	case "executeQuery", "execute", "query":
		return planExecuteQuery(d, cfg, item)

	case "select":
		conds, err := parseConditions(cfg["conditions"])
		if err != nil {
			return sqlStmt{}, err
		}
		sorts, err := parseSort(cfg["sort"])
		if err != nil {
			return sqlStmt{}, err
		}
		rawArgs, err := toList(cfg["params"])
		if err != nil {
			return sqlStmt{}, fmt.Errorf("'params': %w", err)
		}
		limit := 0
		if !cfgBool(cfg, "return_all", false) {
			limit, _ = cfgInt(cfg, "limit")
		}
		return buildSelect(d, selectSpec{
			Schema: schema, Table: table, Columns: toStringList(cfg["output_columns"]),
			Conditions: conds, Combine: cfgString(cfg, "combine_conditions"),
			RawWhere: cfgString(cfg, "where"), RawArgs: rawArgs, Sort: sorts, Limit: limit,
		})

	case "insert", "update", "upsert":
		row, err := buildRow(cfg, item)
		if err != nil {
			return sqlStmt{}, err
		}
		w := writeSpec{
			Schema: schema, Table: table, Row: row,
			MatchColumns:   toStringList(cfg["match_columns"]),
			Returning:      toStringList(cfg["output_columns"]),
			SkipOnConflict: cfgBool(cfg, "skip_on_conflict", false),
		}
		switch op {
		case "insert":
			return buildInsert(d, w)
		case "update":
			return buildUpdate(d, w)
		default:
			return buildUpsert(d, w)
		}

	case "deleteTable":
		conds, err := parseConditions(cfg["conditions"])
		if err != nil {
			return sqlStmt{}, err
		}
		return buildDeleteTable(d, schema, table, cfgString(cfg, "delete_command"), conds,
			cfgString(cfg, "combine_conditions"), cfgBool(cfg, "cascade", false), cfgBool(cfg, "restart_sequences", false))

	case "delete":
		conds, err := parseConditions(cfg["conditions"])
		if err != nil {
			return sqlStmt{}, err
		}
		for _, m := range toStringList(cfg["match_columns"]) {
			v, ok := item.JSON[m]
			if !ok {
				return sqlStmt{}, fmt.Errorf("item has no %q field to match on", m)
			}
			conds = append(conds, sqlCondition{Column: m, Op: "equal", Value: v})
		}
		rawArgs, err := toList(cfg["params"])
		if err != nil {
			return sqlStmt{}, fmt.Errorf("'params': %w", err)
		}
		raw := cfgString(cfg, "where")
		if len(conds) == 0 && raw == "" {
			return sqlStmt{}, fmt.Errorf("delete needs 'match_columns', 'conditions' or 'where' (use deleteTable to empty a table)")
		}
		return buildDeleteRows(d, schema, table, conds, cfgString(cfg, "combine_conditions"), raw, rawArgs)
	}
	return sqlStmt{}, fmt.Errorf("unknown operation %q (want executeQuery, select, insert, update, upsert, delete or deleteTable)", op)
}

func planExecuteQuery(d *sqlDialect, cfg map[string]interface{}, item workflow.Item) (sqlStmt, error) {
	query := cfgString(cfg, "query")
	if query == "" {
		return sqlStmt{}, fmt.Errorf("'query' is required")
	}
	// n8n v2: query_replacement values; n8n v1: query_params_fields names item
	// properties. Both fill $1..$N.
	var dollarArgs []interface{}
	switch v := cfg["query_replacement"].(type) {
	case nil:
	case []interface{}:
		dollarArgs = v
	default:
		for _, s := range toStringList(v) {
			dollarArgs = append(dollarArgs, s)
		}
	}
	for _, f := range toStringList(cfg["query_params_fields"]) {
		dollarArgs = append(dollarArgs, item.JSON[f])
	}
	if len(dollarArgs) > 0 {
		q, args, err := rewriteDollarParams(d, query, dollarArgs)
		if err != nil {
			return sqlStmt{}, err
		}
		return sqlStmt{Query: q, Args: args, Rows: true}, nil
	}
	params, err := toList(cfg["params"])
	if err != nil {
		return sqlStmt{}, fmt.Errorf("'params': %w", err)
	}
	named, err := toList(cfg["bind_params"])
	if err != nil {
		return sqlStmt{}, fmt.Errorf("'bind_params': %w", err)
	}
	for i, e := range named {
		m, ok := e.(map[string]interface{})
		if !ok || cfgString(m, "name") == "" {
			return sqlStmt{}, fmt.Errorf("'bind_params'[%d]: expected {\"name\": ..., \"value\": ...}", i)
		}
		val, ok := m["value"]
		if !ok { // n8n Oracle binds carry a typed value field
			for _, k := range []string{"valueString", "valueNumber", "valueBoolean", "valueDate", "valueJson"} {
				if v, has := m[k]; has {
					val = v
					break
				}
			}
		}
		params = append(params, sql.Named(cfgString(m, "name"), val))
	}
	return sqlStmt{Query: query, Args: params, Rows: true}, nil
}

// buildRow returns the column->value map one item writes. data_mode
// "define" takes 'values' (or the legacy 'data'); "auto_map" (the default
// when neither is set) takes the item's own fields, narrowed to 'columns'
// when given. Match columns always come along.
func buildRow(cfg map[string]interface{}, item workflow.Item) (map[string]interface{}, error) {
	values, err := toKVMap(cfg["values"])
	if err != nil {
		return nil, fmt.Errorf("'values': %w", err)
	}
	if values == nil && cfg["values_to_send"] != nil {
		if values, err = toKVMap(cfg["values_to_send"]); err != nil {
			return nil, fmt.Errorf("'values_to_send': %w", err)
		}
	}
	if values == nil {
		if values, err = toMap(cfg["data"]); err != nil {
			return nil, fmt.Errorf("'data': %w", err)
		}
	}
	mode := strings.ToLower(cfgString(cfg, "data_mode"))
	switch mode {
	case "":
		mode = "auto_map"
		if values != nil {
			mode = "define"
		}
	case "automapinputdata", "auto":
		mode = "auto_map"
	case "definebelow":
		mode = "define"
	}
	row := map[string]interface{}{}
	switch mode {
	case "define":
		for k, v := range values {
			row[k] = v
		}
		if v, ok := cfg["value_to_match_on"]; ok && v != nil {
			if match := toStringList(cfg["match_columns"]); len(match) > 0 {
				row[match[0]] = v
			}
		}
	case "auto_map":
		cols := toStringList(cfg["columns"])
		if len(cols) == 0 {
			for k, v := range item.JSON {
				row[k] = v
			}
		} else {
			for _, c := range cols {
				row[c] = item.JSON[c]
			}
		}
		for _, m := range toStringList(cfg["match_columns"]) {
			if v, ok := item.JSON[m]; ok {
				row[m] = v
			}
		}
	default:
		return nil, fmt.Errorf("'data_mode' must be auto_map or define (got %q)", mode)
	}
	if cfgBool(cfg, "replace_empty_strings", false) {
		for k, v := range row {
			if s, ok := v.(string); ok && s == "" {
				row[k] = nil
			}
		}
	}
	return row, nil
}

// parseConditions reads [{column, condition|operator, value}] (or n8n's
// {"values": [...]} wrapper).
func parseConditions(v interface{}) ([]sqlCondition, error) {
	list, err := toList(v)
	if err != nil {
		return nil, fmt.Errorf("'conditions': %w", err)
	}
	out := make([]sqlCondition, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("'conditions'[%d]: expected an object", i)
		}
		c := sqlCondition{Column: cfgString(m, "column"), Op: cfgString(m, "condition"), Value: m["value"]}
		if c.Op == "" {
			c.Op = cfgString(m, "operator")
		}
		if c.Column == "" {
			return nil, fmt.Errorf("'conditions'[%d]: 'column' is required", i)
		}
		out = append(out, c)
	}
	return out, nil
}

func parseSort(v interface{}) ([]sqlSort, error) {
	list, err := toList(v)
	if err != nil {
		return nil, fmt.Errorf("'sort': %w", err)
	}
	out := make([]sqlSort, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("'sort'[%d]: expected an object", i)
		}
		s := sqlSort{Column: cfgString(m, "column"), Direction: cfgString(m, "direction")}
		if s.Column == "" {
			return nil, fmt.Errorf("'sort'[%d]: 'column' is required", i)
		}
		out = append(out, s)
	}
	return out, nil
}
