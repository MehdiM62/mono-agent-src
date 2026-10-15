package dbnodes

import (
	"fmt"
	"strings"
)

// sqlCondition is one row of n8n's "Select Rows" / where collection.
type sqlCondition struct {
	Column string
	Op     string // equal, !=, LIKE, >, <, >=, <=, IS NULL, IS NOT NULL
	Value  interface{}
}

// sqlSort is one ORDER BY entry.
type sqlSort struct {
	Column    string
	Direction string // ASC | DESC
}

// sqlStmt is one statement ready to run.
type sqlStmt struct {
	Query string
	Args  []interface{}
	Rows  bool // run with Query (rows come back) rather than Exec
}

// argList accumulates bind args and hands out the matching placeholders.
type argList struct {
	d    *sqlDialect
	args []interface{}
}

func (a *argList) add(v interface{}) string {
	a.args = append(a.args, v)
	return a.d.placeholder(len(a.args))
}

// conditionOps maps the accepted operator spellings to SQL.
var conditionOps = map[string]string{
	"equal": "=", "=": "=", "eq": "=",
	"!=": "<>", "<>": "<>", "notequal": "<>", "not_equal": "<>",
	"like": "LIKE", ">": ">", "<": "<", ">=": ">=", "<=": "<=",
	"is null": "IS NULL", "is_null": "IS NULL", "is not null": "IS NOT NULL", "is_not_null": "IS NOT NULL",
}

// buildWhere renders conditions joined by combine (AND/OR), optionally
// AND-ed with a raw where expression whose own args are already in a.
func buildWhere(a *argList, conds []sqlCondition, combine, raw string) (string, error) {
	var parts []string
	for i, c := range conds {
		col, err := a.d.quoteIdent(c.Column)
		if err != nil {
			return "", fmt.Errorf("conditions[%d].column: %w", i, err)
		}
		op, ok := conditionOps[strings.ToLower(strings.TrimSpace(c.Op))]
		if !ok {
			if c.Op == "" {
				op = "="
			} else {
				return "", fmt.Errorf("conditions[%d].condition: unknown operator %q", i, c.Op)
			}
		}
		switch op {
		case "IS NULL", "IS NOT NULL":
			parts = append(parts, col+" "+op)
		default:
			parts = append(parts, fmt.Sprintf("%s %s %s", col, op, a.add(c.Value)))
		}
	}
	joiner := " AND "
	if strings.EqualFold(strings.TrimSpace(combine), "OR") {
		joiner = " OR "
	}
	where := strings.Join(parts, joiner)
	if raw != "" {
		if err := validateWhereClause(raw); err != nil {
			return "", err
		}
		if where == "" {
			return raw, nil
		}
		return "(" + raw + ") AND (" + where + ")", nil
	}
	return where, nil
}

// selectSpec describes a SELECT built from the n8n "select" operation.
type selectSpec struct {
	Schema, Table string
	Columns       []string // output columns; empty = *
	Conditions    []sqlCondition
	Combine       string
	RawWhere      string
	RawArgs       []interface{}
	Sort          []sqlSort
	Limit         int // <= 0 = no limit
}

func buildSelect(d *sqlDialect, s selectSpec) (sqlStmt, error) {
	table, err := d.quoteTable(s.Schema, s.Table)
	if err != nil {
		return sqlStmt{}, err
	}
	cols, err := quoteColumnList(d, s.Columns, "*")
	if err != nil {
		return sqlStmt{}, fmt.Errorf("output_columns: %w", err)
	}
	a := &argList{d: d, args: append([]interface{}(nil), s.RawArgs...)}
	where, err := buildWhere(a, s.Conditions, s.Combine, s.RawWhere)
	if err != nil {
		return sqlStmt{}, err
	}
	var q strings.Builder
	q.WriteString("SELECT ")
	if s.Limit > 0 && d.limit == limitTop {
		fmt.Fprintf(&q, "TOP (%d) ", s.Limit)
	}
	q.WriteString(cols + " FROM " + table)
	if where != "" {
		q.WriteString(" WHERE " + where)
	}
	if len(s.Sort) > 0 {
		var order []string
		for i, o := range s.Sort {
			c, err := d.quoteIdent(o.Column)
			if err != nil {
				return sqlStmt{}, fmt.Errorf("sort[%d].column: %w", i, err)
			}
			dir := strings.ToUpper(strings.TrimSpace(o.Direction))
			if dir != "DESC" {
				dir = "ASC"
			}
			order = append(order, c+" "+dir)
		}
		q.WriteString(" ORDER BY " + strings.Join(order, ", "))
	}
	if s.Limit > 0 {
		switch d.limit {
		case limitClause:
			fmt.Fprintf(&q, " LIMIT %d", s.Limit)
		case limitFetch:
			fmt.Fprintf(&q, " FETCH FIRST %d ROWS ONLY", s.Limit)
		}
	}
	return sqlStmt{Query: q.String(), Args: a.args, Rows: true}, nil
}

func quoteColumnList(d *sqlDialect, cols []string, empty string) (string, error) {
	if len(cols) == 0 || (len(cols) == 1 && cols[0] == "*") {
		return empty, nil
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		q, err := d.quoteIdent(c)
		if err != nil {
			return "", err
		}
		out[i] = q
	}
	return strings.Join(out, ", "), nil
}

// returningClause renders " RETURNING ..." for dialects that support it.
func returningClause(d *sqlDialect, cols []string) (string, error) {
	if !d.returning {
		return "", nil
	}
	list, err := quoteColumnList(d, cols, "*")
	if err != nil {
		return "", fmt.Errorf("output_columns: %w", err)
	}
	return " RETURNING " + list, nil
}

// writeSpec describes one row written by insert/update/upsert.
type writeSpec struct {
	Schema, Table  string
	Row            map[string]interface{} // column -> value
	MatchColumns   []string               // update/upsert key columns (values come from Row)
	Returning      []string               // output columns for RETURNING
	SkipOnConflict bool                   // insert: ignore rows that violate a unique key
}

func buildInsert(d *sqlDialect, w writeSpec) (sqlStmt, error) {
	table, err := d.quoteTable(w.Schema, w.Table)
	if err != nil {
		return sqlStmt{}, err
	}
	if len(w.Row) == 0 {
		return sqlStmt{}, fmt.Errorf("insert: no columns to write")
	}
	a := &argList{d: d}
	cols, vals, err := columnsAndPlaceholders(a, w.Row)
	if err != nil {
		return sqlStmt{}, err
	}
	verb := "INSERT INTO "
	suffix := ""
	if w.SkipOnConflict {
		switch d.upsert {
		case upsertOnDuplicate:
			verb = "INSERT IGNORE INTO "
		case upsertOnConflict:
			suffix = " ON CONFLICT DO NOTHING"
		}
	}
	ret, err := returningClause(d, w.Returning)
	if err != nil {
		return sqlStmt{}, err
	}
	q := fmt.Sprintf("%s%s (%s) VALUES (%s)%s%s", verb, table, strings.Join(cols, ", "), strings.Join(vals, ", "), suffix, ret)
	return sqlStmt{Query: q, Args: a.args, Rows: ret != ""}, nil
}

func columnsAndPlaceholders(a *argList, row map[string]interface{}) ([]string, []string, error) {
	cols := make([]string, 0, len(row))
	vals := make([]string, 0, len(row))
	for _, k := range sortedMapKeys(row) {
		c, err := a.d.quoteIdent(k)
		if err != nil {
			return nil, nil, fmt.Errorf("column %q: %w", k, err)
		}
		cols = append(cols, c)
		vals = append(vals, a.add(row[k]))
	}
	return cols, vals, nil
}

// splitRow separates the match columns (and their values) from the rest.
func splitRow(w writeSpec) (set map[string]interface{}, match []string, err error) {
	if len(w.MatchColumns) == 0 {
		return nil, nil, fmt.Errorf("'match_columns' is required")
	}
	isMatch := map[string]bool{}
	for _, m := range w.MatchColumns {
		if _, ok := w.Row[m]; !ok {
			return nil, nil, fmt.Errorf("no value for match column %q", m)
		}
		isMatch[m] = true
	}
	set = map[string]interface{}{}
	for k, v := range w.Row {
		if !isMatch[k] {
			set[k] = v
		}
	}
	return set, w.MatchColumns, nil
}

func buildUpdate(d *sqlDialect, w writeSpec) (sqlStmt, error) {
	table, err := d.quoteTable(w.Schema, w.Table)
	if err != nil {
		return sqlStmt{}, err
	}
	set, match, err := splitRow(w)
	if err != nil {
		return sqlStmt{}, fmt.Errorf("update: %w", err)
	}
	if len(set) == 0 {
		return sqlStmt{}, fmt.Errorf("update: no columns to update besides the match column(s)")
	}
	a := &argList{d: d}
	var sets []string
	for _, k := range sortedMapKeys(set) {
		c, err := d.quoteIdent(k)
		if err != nil {
			return sqlStmt{}, fmt.Errorf("column %q: %w", k, err)
		}
		sets = append(sets, c+" = "+a.add(set[k]))
	}
	var conds []string
	for _, m := range match {
		c, err := d.quoteIdent(m)
		if err != nil {
			return sqlStmt{}, fmt.Errorf("match column %q: %w", m, err)
		}
		conds = append(conds, c+" = "+a.add(w.Row[m]))
	}
	ret, err := returningClause(d, w.Returning)
	if err != nil {
		return sqlStmt{}, err
	}
	q := fmt.Sprintf("UPDATE %s SET %s WHERE %s%s", table, strings.Join(sets, ", "), strings.Join(conds, " AND "), ret)
	return sqlStmt{Query: q, Args: a.args, Rows: ret != ""}, nil
}

func buildUpsert(d *sqlDialect, w writeSpec) (sqlStmt, error) {
	table, err := d.quoteTable(w.Schema, w.Table)
	if err != nil {
		return sqlStmt{}, err
	}
	set, match, err := splitRow(w)
	if err != nil {
		return sqlStmt{}, fmt.Errorf("upsert: %w", err)
	}
	quoted := func(names []string) ([]string, error) {
		out := make([]string, len(names))
		for i, n := range names {
			q, err := d.quoteIdent(n)
			if err != nil {
				return nil, fmt.Errorf("column %q: %w", n, err)
			}
			out[i] = q
		}
		return out, nil
	}
	setCols, err := quoted(sortedMapKeys(set))
	if err != nil {
		return sqlStmt{}, err
	}
	matchCols, err := quoted(match)
	if err != nil {
		return sqlStmt{}, err
	}
	a := &argList{d: d}
	switch d.upsert {
	case upsertOnConflict, upsertOnDuplicate:
		cols, vals, err := columnsAndPlaceholders(a, w.Row)
		if err != nil {
			return sqlStmt{}, err
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(vals, ", "))
		var upd []string
		for _, c := range setCols {
			if d.upsert == upsertOnConflict {
				upd = append(upd, c+" = EXCLUDED."+c)
			} else {
				upd = append(upd, c+" = VALUES("+c+")")
			}
		}
		if d.upsert == upsertOnConflict {
			if len(upd) == 0 {
				q += " ON CONFLICT (" + strings.Join(matchCols, ", ") + ") DO NOTHING"
			} else {
				q += " ON CONFLICT (" + strings.Join(matchCols, ", ") + ") DO UPDATE SET " + strings.Join(upd, ", ")
			}
		} else {
			if len(upd) == 0 { // MySQL needs an UPDATE list; a self-assignment is a no-op
				upd = append(upd, matchCols[0]+" = "+matchCols[0])
			}
			q += " ON DUPLICATE KEY UPDATE " + strings.Join(upd, ", ")
		}
		ret, err := returningClause(d, w.Returning)
		if err != nil {
			return sqlStmt{}, err
		}
		return sqlStmt{Query: q + ret, Args: a.args, Rows: ret != ""}, nil

	case upsertMerge, upsertMergeDual:
		allKeys := sortedMapKeys(w.Row)
		allCols, err := quoted(allKeys)
		if err != nil {
			return sqlStmt{}, err
		}
		var src []string
		for i, k := range allKeys {
			src = append(src, a.add(w.Row[k])+" AS "+allCols[i])
		}
		using := "SELECT " + strings.Join(src, ", ")
		if d.upsert == upsertMergeDual {
			using += " FROM dual"
		}
		var on []string
		for _, c := range matchCols {
			on = append(on, "tgt."+c+" = src."+c)
		}
		var srcVals []string
		for _, c := range allCols {
			srcVals = append(srcVals, "src."+c)
		}
		asKw := " AS "
		if d.upsert == upsertMergeDual {
			asKw = " " // Oracle does not accept AS before a table alias
		}
		q := fmt.Sprintf("MERGE INTO %s%stgt USING (%s)%ssrc ON (%s)", table, asKw, using, asKw, strings.Join(on, " AND "))
		if len(setCols) > 0 {
			var upd []string
			for _, c := range setCols {
				upd = append(upd, "tgt."+c+" = src."+c)
			}
			q += " WHEN MATCHED THEN UPDATE SET " + strings.Join(upd, ", ")
		}
		q += fmt.Sprintf(" WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s)", strings.Join(allCols, ", "), strings.Join(srcVals, ", "))
		if d.name == "mssql" {
			q += ";" // SQL Server requires MERGE to be terminated
		}
		return sqlStmt{Query: q, Args: a.args}, nil
	}
	return sqlStmt{}, fmt.Errorf("upsert is not supported by %s", d.name)
}

// buildDeleteRows renders DELETE FROM ... WHERE for conditions / match keys.
func buildDeleteRows(d *sqlDialect, schema, table string, conds []sqlCondition, combine, raw string, rawArgs []interface{}) (sqlStmt, error) {
	t, err := d.quoteTable(schema, table)
	if err != nil {
		return sqlStmt{}, err
	}
	a := &argList{d: d, args: append([]interface{}(nil), rawArgs...)}
	where, err := buildWhere(a, conds, combine, raw)
	if err != nil {
		return sqlStmt{}, err
	}
	q := "DELETE FROM " + t
	if where != "" {
		q += " WHERE " + where
	}
	return sqlStmt{Query: q, Args: a.args}, nil
}

// buildDeleteTable renders n8n's deleteTable operation: truncate, delete
// (rows matching the conditions; all rows when none are given) or drop.
func buildDeleteTable(d *sqlDialect, schema, table, command string, conds []sqlCondition, combine string, cascade, restart bool) (sqlStmt, error) {
	t, err := d.quoteTable(schema, table)
	if err != nil {
		return sqlStmt{}, err
	}
	switch strings.ToLower(command) {
	case "", "truncate":
		return sqlStmt{Query: d.truncate(t, cascade, restart)}, nil
	case "delete":
		return buildDeleteRows(d, schema, table, conds, combine, "", nil)
	case "drop":
		return sqlStmt{Query: d.drop(t, cascade)}, nil
	}
	return sqlStmt{}, fmt.Errorf("'delete_command' must be truncate, delete or drop (got %q)", command)
}
