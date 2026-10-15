package dbnodes

import (
	"fmt"
	"regexp"
	"strings"
)

// limitStyle is how a dialect caps a SELECT's row count.
type limitStyle int

const (
	limitClause limitStyle = iota // ... LIMIT n
	limitTop                      // SELECT TOP (n) ...
	limitFetch                    // ... FETCH FIRST n ROWS ONLY
)

// upsertStyle is how a dialect expresses insert-or-update.
type upsertStyle int

const (
	upsertOnConflict  upsertStyle = iota // INSERT ... ON CONFLICT (k) DO UPDATE (Postgres family)
	upsertOnDuplicate                    // INSERT ... ON DUPLICATE KEY UPDATE (MySQL/MariaDB)
	upsertMerge                          // MERGE INTO ... USING (SELECT ...) (SQL Server, Snowflake)
	upsertMergeDual                      // MERGE INTO ... USING (SELECT ... FROM dual) (Oracle)
	upsertUnsupported                    // no portable upsert (QuestDB)
)

// sqlDialect captures everything the statement builder needs to know about
// one SQL engine: identifier quoting, bind placeholders, and the syntax of
// LIMIT, RETURNING, TRUNCATE and upsert.
type sqlDialect struct {
	name        string
	quoteChar   string             // opening quote; closing is quoteClose (or quoteChar)
	quoteClose  string             // "" = same as quoteChar
	foldPlain   bool               // leave plain identifiers unquoted (Oracle/Snowflake fold them to upper case)
	placeholder func(n int) string // 1-based bind placeholder
	reorderArgs bool               // placeholders are positional by appearance ("?"), so $N must be expanded in order
	returning   bool               // supports INSERT/UPDATE ... RETURNING
	limit       limitStyle         //
	upsert      upsertStyle        //
	truncate    func(t string, cascade, restart bool) string
	drop        func(t string, cascade bool) string
}

var plainIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

// quoteIdent quotes one identifier part, doubling any embedded closing quote.
// Empty names and names containing NUL are rejected.
func (d *sqlDialect) quoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	if d.foldPlain && plainIdentifier.MatchString(name) {
		return name, nil
	}
	closeQ := d.quoteClose
	if closeQ == "" {
		closeQ = d.quoteChar
	}
	return d.quoteChar + strings.ReplaceAll(name, closeQ, closeQ+closeQ) + closeQ, nil
}

// quoteTable quotes an optionally schema-qualified table. A table name with a
// dot and no explicit schema is read as "schema.table".
func (d *sqlDialect) quoteTable(schema, table string) (string, error) {
	if table == "" {
		return "", fmt.Errorf("'table' is required")
	}
	if schema == "" && strings.Count(table, ".") == 1 && !strings.ContainsAny(table, "\"`[]") {
		parts := strings.SplitN(table, ".", 2)
		schema, table = parts[0], parts[1]
	}
	t, err := d.quoteIdent(table)
	if err != nil {
		return "", fmt.Errorf("invalid table: %w", err)
	}
	if schema == "" {
		return t, nil
	}
	s, err := d.quoteIdent(schema)
	if err != nil {
		return "", fmt.Errorf("invalid schema: %w", err)
	}
	return s + "." + t, nil
}

func dollarPlaceholder(n int) string   { return fmt.Sprintf("$%d", n) }
func questionPlaceholder(int) string   { return "?" }
func atPPlaceholder(n int) string      { return fmt.Sprintf("@p%d", n) }
func colonNumPlaceholder(n int) string { return fmt.Sprintf(":%d", n) }

func pgTruncate(t string, cascade, restart bool) string {
	q := "TRUNCATE TABLE " + t
	if restart {
		q += " RESTART IDENTITY"
	}
	if cascade {
		q += " CASCADE"
	}
	return q
}

func pgDrop(t string, cascade bool) string {
	q := "DROP TABLE IF EXISTS " + t
	if cascade {
		q += " CASCADE"
	}
	return q
}

func plainTruncate(t string, _, _ bool) string  { return "TRUNCATE TABLE " + t }
func plainDropIfExists(t string, _ bool) string { return "DROP TABLE IF EXISTS " + t }

var (
	postgresDialect = &sqlDialect{
		name: "postgres", quoteChar: `"`, placeholder: dollarPlaceholder,
		returning: true, limit: limitClause, upsert: upsertOnConflict,
		truncate: pgTruncate, drop: pgDrop,
	}
	// CrateDB speaks the Postgres wire protocol and supports ON CONFLICT and
	// RETURNING, but not TRUNCATE's RESTART IDENTITY / CASCADE options.
	crateDialect = &sqlDialect{
		name: "cratedb", quoteChar: `"`, placeholder: dollarPlaceholder,
		returning: true, limit: limitClause, upsert: upsertOnConflict,
		truncate: func(t string, _, _ bool) string { return "DELETE FROM " + t },
		drop:     plainDropIfExists,
	}
	// QuestDB speaks the Postgres wire protocol but has no RETURNING and no
	// ON CONFLICT upsert.
	questDialect = &sqlDialect{
		name: "questdb", quoteChar: `"`, placeholder: dollarPlaceholder,
		returning: false, limit: limitClause, upsert: upsertUnsupported,
		truncate: plainTruncate, drop: plainDropIfExists,
	}
	mysqlDialect = &sqlDialect{
		name: "mysql", quoteChar: "`", placeholder: questionPlaceholder, reorderArgs: true,
		returning: false, limit: limitClause, upsert: upsertOnDuplicate,
		truncate: plainTruncate, drop: plainDropIfExists,
	}
	mssqlDialect = &sqlDialect{
		name: "mssql", quoteChar: "[", quoteClose: "]", placeholder: atPPlaceholder,
		returning: false, limit: limitTop, upsert: upsertMerge,
		truncate: plainTruncate, drop: plainDropIfExists,
	}
	oracleDialect = &sqlDialect{
		name: "oracle", quoteChar: `"`, foldPlain: true, placeholder: colonNumPlaceholder, reorderArgs: true,
		returning: false, limit: limitFetch, upsert: upsertMergeDual,
		truncate: plainTruncate,
		drop: func(t string, cascade bool) string {
			q := "DROP TABLE " + t
			if cascade {
				q += " CASCADE CONSTRAINTS"
			}
			return q
		},
	}
	snowflakeDialect = &sqlDialect{
		name: "snowflake", quoteChar: `"`, foldPlain: true, placeholder: questionPlaceholder, reorderArgs: true,
		returning: false, limit: limitClause, upsert: upsertMerge,
		truncate: plainTruncate,
		drop: func(t string, cascade bool) string {
			q := "DROP TABLE IF EXISTS " + t
			if cascade {
				q += " CASCADE"
			}
			return q
		},
	}
)

// rewriteDollarParams rewrites n8n-style $1..$N query parameters into the
// dialect's placeholders and returns the bind args in the order the rewritten
// query needs them. Text inside single-quoted literals, double-quoted
// identifiers and comments is left untouched. For "$N"-native dialects the
// query is returned unchanged.
func rewriteDollarParams(d *sqlDialect, query string, args []interface{}) (string, []interface{}, error) {
	if d.placeholder(1) == "$1" || len(args) == 0 {
		return query, args, nil
	}
	var b strings.Builder
	var out []interface{}
	seq := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
			b.WriteByte(c)
			continue
		case inDouble:
			if c == '"' {
				inDouble = false
			}
			b.WriteByte(c)
			continue
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '$' && i+1 < len(query) && query[i+1] >= '1' && query[i+1] <= '9':
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				j++
			}
			var n int
			fmt.Sscanf(query[i+1:j], "%d", &n)
			if n > len(args) {
				return "", nil, fmt.Errorf("query references $%d but only %d query parameter(s) were given", n, len(args))
			}
			if d.reorderArgs {
				seq++
				b.WriteString(d.placeholder(seq))
				out = append(out, args[n-1])
			} else {
				b.WriteString(d.placeholder(n))
			}
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	if !d.reorderArgs {
		out = args
	}
	return b.String(), out, nil
}
