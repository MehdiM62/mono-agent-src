package dbnodes

// Code layout: schema-only companion structs for the node types added for
// n8n parity. internal/workflow/schemas/<type>.json is rendered from them.

// credential_platform: mssql
//
// MSSQLNodeSchema documents db.mssql.
type MSSQLNodeSchema struct {
	ConnectionString    string  `json:"connection_string" schema:"label=Connection String,type=password,required,help=Usually filled from the connection (credential_id)."`
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|upsert|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
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

// credential_platform: oracle
//
// OracleNodeSchema documents db.oracle.
type OracleNodeSchema struct {
	ConnectionString    string  `json:"connection_string" schema:"label=Connection String,type=password,required,help=Usually filled from the connection (credential_id)."`
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|upsert|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	BindParams          string  `json:"bind_params" schema:"label=Named Bind Parameters (JSON),type=textarea,rows=3,placeholder=[{\"name\": \"id\"， \"value\": 1}],help=Bound as :name.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
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

// credential_platform: timescaledb
//
// TimescaleDBNodeSchema documents db.timescaledb.
type TimescaleDBNodeSchema struct {
	ConnectionString    string  `json:"connection_string" schema:"label=Connection String,type=password,required,help=Usually filled from the connection (credential_id)."`
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|upsert|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
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
	RestartSequences    bool    `json:"restart_sequences" schema:"label=Restart Sequences,type=boolean,depends_on_key=delete_command,depends_on_values=truncate"`
	SkipOnConflict      bool    `json:"skip_on_conflict" schema:"label=Skip on Conflict,type=boolean,depends_on_key=operation,depends_on_values=insert"`
	ReplaceEmptyStrings bool    `json:"replace_empty_strings" schema:"label=Replace Empty Strings with NULL,type=boolean,depends_on_key=operation,depends_on_values=insert|update|upsert"`
	QueryBatching       string  `json:"query_batching" schema:"label=Query Batching,type=select,options=single|transaction|independently,help=single/transaction: all items in one transaction (blank = single); independently: failures go to the error output."`
}

// credential_platform: cratedb
//
// CrateDBNodeSchema documents db.cratedb.
type CrateDBNodeSchema struct {
	ConnectionString    string  `json:"connection_string" schema:"label=Connection String,type=password,required,help=Usually filled from the connection (credential_id)."`
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|upsert|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
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

// credential_platform: questdb
//
// QuestDBNodeSchema documents db.questdb (no upsert).
type QuestDBNodeSchema struct {
	ConnectionString    string  `json:"connection_string" schema:"label=Connection String,type=password,required,help=Usually filled from the connection (credential_id)."`
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	DataMode            string  `json:"data_mode" schema:"label=Data Mode,type=select,options=auto_map|define,help=auto_map writes the input item's fields as columns; define writes Values below.,depends_on_key=operation,depends_on_values=insert|update"`
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

// credential_platform: snowflake
//
// SnowflakeNodeSchema documents db.snowflake (account and auth come from the connection).
type SnowflakeNodeSchema struct {
	Operation           string  `json:"operation" schema:"label=Operation,type=select,options=executeQuery|select|insert|update|upsert|delete|deleteTable,help=executeQuery runs SQL; select/insert/update/upsert/delete/deleteTable build it for you. Each runs once per input item."`
	Query               string  `json:"query" schema:"label=SQL Query,type=code,language=sql,placeholder=SELECT * FROM users WHERE id = $1,depends_on_key=operation,depends_on_values=executeQuery|execute|select"`
	QueryReplacement    string  `json:"query_replacement" schema:"label=Query Parameters,type=text,placeholder={{ $json.id }}， {{ $json.email }},help=Values for $1， $2 … (comma-separated or a JSON array). $N works on every database.,depends_on_key=operation,depends_on_values=executeQuery"`
	Schema              string  `json:"schema" schema:"label=Schema,type=text,help=Optional; a dotted table name (schema.table) works too.,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
	Table               string  `json:"table" schema:"label=Table,type=text,depends_on_key=operation,depends_on_values=select|insert|update|upsert|delete|deleteTable"`
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
