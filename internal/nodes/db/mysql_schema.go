package dbnodes

// credential_platform: mysql
//
// MySQLNodeSchema documents the config keys MySQLNode.Execute reads out of
// its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// The hand-written schemas/db.mysql.json this replaces exposed only "query"
// (marked required) and "params" — but MySQLNode.Execute has always
// supported a second mode: when "query" is empty and "table" is set, it
// builds INSERT/UPDATE/DELETE/SELECT statements from "operation", "table",
// "data", and "where" (see buildMySQLQuery). That whole table-builder mode
// had no schema fields at all, so it was unreachable from the UI. Fixed
// here by adding operation/table/data/where, and by dropping "required"
// from "query" since a table-mode config can leave it blank (Execute itself
// still errors if neither "query" nor "table" is set — the schema can't
// express that either/or requirement, so this documents it via help text
// instead).
type MySQLNodeSchema struct {
	ConnectionString string `json:"connection_string" schema:"label=Connection String,type=password,required,placeholder=user:pass@tcp(host:3306)/db"`

	Operation string `json:"operation" schema:"label=Operation,type=select,options=select|insert|update|delete|execute|executeQuery|upsert|deleteTable,default=select,help=Legacy mode: select/insert/update/delete/execute with Table/Data/Where or a raw SQL Query run once. n8n mode (executeQuery/upsert/deleteTable or any n8n field below): runs once per input item."`

	Query string `json:"query" schema:"label=SQL Query,type=code,placeholder=SELECT * FROM users WHERE active = ?,help=Either this or Table must be set. Takes precedence over Table/Data/Where when both are given."`

	Params string `json:"params" schema:"label=Query Parameters (JSON array),type=textarea,rows=3,placeholder=[1]"`

	Table string `json:"table" schema:"label=Table,type=text,help=Used with Operation/Data/Where to build a query when SQL Query is left blank."`

	Data string `json:"data" schema:"label=Row Data (JSON object),type=textarea,rows=3,placeholder={ \"name\": \"Ada\" },help=Column/value pairs for insert or update， used only in table-builder mode.,depends_on_key=operation,depends_on_values=insert|update"`

	Where string `json:"where" schema:"label=Where Clause,type=text,placeholder=id = ?,help=Raw SQL boolean expression (no leading WHERE). Required for update/delete in table-builder mode.,depends_on_key=operation,depends_on_values=select|update|delete"`

	// n8n-compatible mode (see isN8nSQLConfig).
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	DataMode            string  `json:"data_mode" schema:"label=Data Mode,type=select,options=auto_map|define,help=auto_map writes the input item's fields as columns; define writes Values below.,depends_on_key=operation,depends_on_values=insert|update|upsert"`
	Values              string  `json:"values" schema:"label=Values (JSON object),type=textarea,rows=4,placeholder={\"name\": \"{{ $json.name }}\"},help=Column/value pairs (define mode).,depends_on_key=data_mode,depends_on_values=define"`
	Columns             string  `json:"columns" schema:"label=Columns,type=text,placeholder=name， email,help=Only write these item fields (auto_map mode; blank = all fields).,depends_on_key=data_mode,depends_on_values=auto_map"`
	MatchColumns        string  `json:"match_columns" schema:"label=Column(s) to Match On,type=text,placeholder=id,help=Key column(s) that identify the row. Comma-separated.,depends_on_key=operation,depends_on_values=update|upsert|delete"`
	ValueToMatchOn      string  `json:"value_to_match_on" schema:"label=Value to Match On,type=text,help=define mode: value for the first match column.,depends_on_key=operation,depends_on_values=update|upsert"`
	Conditions          string  `json:"conditions" schema:"label=Conditions (JSON array),type=textarea,rows=4,placeholder=[{\"column\": \"status\"， \"condition\": \"equal\"， \"value\": \"open\"}],help=Operators: equal， !=， LIKE， >， <， >=， <=， IS NULL， IS NOT NULL.,depends_on_key=operation,depends_on_values=select|delete|deleteTable"`
	CombineConditions   string  `json:"combine_conditions" schema:"label=Combine Conditions,type=select,options=AND|OR,depends_on_key=operation,depends_on_values=select|delete|deleteTable"`
	Sort                string  `json:"sort" schema:"label=Sort (JSON array),type=textarea,rows=2,placeholder=[{\"column\": \"created_at\"， \"direction\": \"DESC\"}],depends_on_key=operation,depends_on_values=select"`
	Limit               float64 `json:"limit" schema:"label=Limit,type=number,help=Blank or 0 = no limit.,depends_on_key=operation,depends_on_values=select"`
	ReturnAll           bool    `json:"return_all" schema:"label=Return All,type=boolean,depends_on_key=operation,depends_on_values=select"`
	OutputColumns       string  `json:"output_columns" schema:"label=Output Columns,type=text,placeholder=id， name,help=Columns to return (blank = all)."`
	DeleteCommand       string  `json:"delete_command" schema:"label=Command,type=select,options=truncate|delete|drop,depends_on_key=operation,depends_on_values=deleteTable"`
	Cascade             bool    `json:"cascade" schema:"label=Cascade,type=boolean,depends_on_key=operation,depends_on_values=deleteTable"`
	ReplaceEmptyStrings bool    `json:"replace_empty_strings" schema:"label=Replace Empty Strings with NULL,type=boolean,depends_on_key=operation,depends_on_values=insert|update|upsert"`
	QueryBatching       string  `json:"query_batching" schema:"label=Query Batching,type=select,options=single|transaction|independently,help=single/transaction: all items in one transaction (blank = single); independently: failures go to the error output."`
}
