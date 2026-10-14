package control

// DataTableNodeSchema documents the config keys DataTableNode.Execute reads;
// see SetNodeSchema's doc comment for why this is a companion struct.
type DataTableNodeSchema struct {
	Resource string `json:"resource" schema:"label=Resource,type=select,options=row|table,default=row"`

	Operation string `json:"operation" schema:"label=Operation,type=select,options=insert|get|update|upsert|deleteRows|rowExists|rowNotExists|create|list|clear|delete,default=insert,help=Rows: insert / get / update / upsert / deleteRows / rowExists (passes items that match) / rowNotExists. Tables: create / list / update (rename) / clear / delete."`

	Table string `json:"table" schema:"label=Data Table,type=text,placeholder=Leads,help=The table's name or id (for create: the new table's name). Resolved per item."`

	ColumnsMode string `json:"columns_mode" schema:"label=Columns,type=select,options=values|auto,default=values,help=values sets the columns below; auto writes each item's JSON (without id/createdAt/updatedAt).,depends_on_key=operation,depends_on_values=insert|update|upsert"`

	Values string `json:"values" schema:"label=Values,type=code,language=json,rows=4,placeholder={\"email\": \"{{ $json.email }}\"， \"score\": \"{{ $json.score }}\"},help=Column → value， resolved per item. A column the table lacks is added， typed by its value.,depends_on_key=operation,depends_on_values=insert|update|upsert"`

	Match string `json:"match" schema:"label=Must Match,type=select,options=any|all,default=any,depends_on_key=operation,depends_on_values=get|update|upsert|deleteRows|rowExists|rowNotExists"`

	Filters string `json:"filters" schema:"label=Conditions,type=code,language=json,rows=4,placeholder=[{\"column\": \"email\"， \"condition\": \"eq\"， \"value\": \"{{ $json.email }}\"}],help=Conditions eq (default)， neq， gt， gte， lt， lte， like / ilike (% is the wildcard; without one the value matches anywhere)， isEmpty， isNotEmpty， isTrue， isFalse; resolved per item. id， createdAt and updatedAt can be filtered too. Required except for get.,depends_on_key=operation,depends_on_values=get|update|upsert|deleteRows|rowExists|rowNotExists"`

	ReturnAll bool `json:"return_all" schema:"label=Return All,type=boolean,help=get: all matching rows (default: up to Limit). list: all tables (the default).,depends_on_key=operation,depends_on_values=get|list"`

	Limit float64 `json:"limit" schema:"label=Limit,type=number,default=50,min=1,depends_on_key=operation,depends_on_values=get|list"`

	OrderBy string `json:"order_by" schema:"label=Order By Column,type=text,placeholder=createdAt,help=Sort the rows by this column (default: by id).,depends_on_key=operation,depends_on_values=get"`

	OrderDirection string `json:"order_direction" schema:"label=Order Direction,type=select,options=ASC|DESC,default=DESC,depends_on_key=operation,depends_on_values=get"`

	DryRun bool `json:"dry_run" schema:"label=Dry Run,type=boolean,help=Return the affected rows before and after (dryRunState) without writing.,depends_on_key=operation,depends_on_values=update|upsert|deleteRows"`

	OptimizeBulk bool `json:"optimize_bulk" schema:"label=Optimize Bulk,type=boolean,help=Output only {success， insertedRows} instead of the inserted rows.,depends_on_key=operation,depends_on_values=insert"`

	Columns string `json:"columns" schema:"label=Table Columns,type=code,language=json,rows=4,placeholder=[{\"name\": \"email\"， \"type\": \"string\"}],help=The new table's columns: type string， number， boolean or date.,depends_on_key=operation,depends_on_values=create"`

	ReuseExisting bool `json:"reuse_existing" schema:"label=Reuse Existing Table,type=boolean,default=true,help=Return the table with this name if it exists instead of failing.,depends_on_key=operation,depends_on_values=create"`

	NewName string `json:"new_name" schema:"label=New Name,type=text,depends_on_key=operation,depends_on_values=update"`

	FilterName string `json:"filter_name" schema:"label=Filter by Name,type=text,help=Only tables whose name contains this (any case).,depends_on_key=operation,depends_on_values=list"`

	SortField string `json:"sort_field" schema:"label=Sort Field,type=select,options=name|createdAt|updatedAt,default=name,depends_on_key=operation,depends_on_values=list"`

	SortDirection string `json:"sort_direction" schema:"label=Sort Direction,type=select,options=asc|desc,default=asc,depends_on_key=operation,depends_on_values=list"`
}
