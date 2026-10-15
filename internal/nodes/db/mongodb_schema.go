package dbnodes

// credential_platform: mongodb
//
// MongoDBNodeSchema documents the config keys MongoDBNode.Execute reads out
// of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// This matches the hand-written schemas/db.mongodb.json field-for-field.
type MongoDBNodeSchema struct {
	ConnectionString string `json:"connection_string" schema:"label=MongoDB Connection String,type=password,required,placeholder=mongodb://user:pass@host:27017/db"`

	Database string `json:"database" schema:"label=Database,type=text,required"`

	Collection string `json:"collection" schema:"label=Collection,type=text,required"`

	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=find|insert_one|insert_many|update_one|update_many|delete_one|delete_many|aggregate|delete|insert|update|findOneAndReplace|findOneAndUpdate|listSearchIndexes|createSearchIndex|dropSearchIndex|updateSearchIndex,default=find,help=delete/insert/update/findOneAndReplace/findOneAndUpdate and the search-index operations (and find/aggregate with Query) run once per input item the way n8n does."`

	Filter string `json:"filter" schema:"label=Filter (JSON),type=textarea,rows=3,placeholder={ \"status\": \"active\" }"`

	Update string `json:"update" schema:"label=Update (JSON),type=textarea,rows=3,depends_on_key=operation,depends_on_values=update_one|update_many"`

	Document string `json:"document" schema:"label=Document (JSON),type=textarea,rows=3,depends_on_key=operation,depends_on_values=insert_one|insert_many"`

	Pipeline string `json:"pipeline" schema:"label=Aggregation Pipeline (JSON array),type=textarea,rows=3,depends_on_key=operation,depends_on_values=aggregate"`

	Limit float64 `json:"limit" schema:"label=Limit,type=number,depends_on_key=operation,depends_on_values=find"`

	// n8n-compatible mode (see isN8nMongoConfig).
	Query string `json:"query" schema:"label=Query (JSON),type=code,language=json,rows=4,placeholder={ \"age\": { \"$gt\": \"$1\" } },help=Filter for find/delete， pipeline array for aggregate. Extended JSON ($oid， $date) works.,depends_on_key=operation,depends_on_values=find|aggregate|delete"`

	QueryParameters string `json:"query_parameters" schema:"label=Query Parameters (JSON array),type=text,placeholder=[30],help=Values for \"$1\"， \"$2\" … strings in the query.,depends_on_key=operation,depends_on_values=find|aggregate|delete"`

	Skip float64 `json:"skip" schema:"label=Skip,type=number,depends_on_key=operation,depends_on_values=find"`

	Sort string `json:"sort" schema:"label=Sort (JSON),type=text,placeholder={ \"created\": -1 },depends_on_key=operation,depends_on_values=find"`

	Projection string `json:"projection" schema:"label=Projection (JSON),type=text,placeholder={ \"name\": 1 },depends_on_key=operation,depends_on_values=find"`

	Fields string `json:"fields" schema:"label=Fields,type=text,placeholder=name， email,help=Item fields to write (blank = all).,depends_on_key=operation,depends_on_values=insert|update|findOneAndReplace|findOneAndUpdate"`

	UpdateKey string `json:"update_key" schema:"label=Update Key,type=text,placeholder=_id,help=Item field that matches the document to update.,depends_on_key=operation,depends_on_values=update|findOneAndReplace|findOneAndUpdate"`

	Upsert bool `json:"upsert" schema:"label=Upsert,type=boolean,depends_on_key=operation,depends_on_values=update|findOneAndReplace|findOneAndUpdate"`

	DateFields string `json:"date_fields" schema:"label=Date Fields,type=text,help=Fields to store as dates (RFC 3339 strings).,depends_on_key=operation,depends_on_values=insert|update|findOneAndReplace|findOneAndUpdate"`

	UseDotNotation bool `json:"use_dot_notation" schema:"label=Use Dot Notation,type=boolean,depends_on_key=operation,depends_on_values=insert|update|findOneAndReplace|findOneAndUpdate"`

	IndexName string `json:"index_name" schema:"label=Index Name,type=text,depends_on_key=operation,depends_on_values=listSearchIndexes|createSearchIndex|dropSearchIndex|updateSearchIndex"`

	IndexDefinition string `json:"index_definition" schema:"label=Index Definition (JSON),type=code,language=json,rows=4,depends_on_key=operation,depends_on_values=createSearchIndex|updateSearchIndex"`

	IndexType string `json:"index_type" schema:"label=Index Type,type=select,options=search|vectorSearch,depends_on_key=operation,depends_on_values=createSearchIndex"`
}
