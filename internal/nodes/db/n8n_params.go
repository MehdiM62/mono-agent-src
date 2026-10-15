package dbnodes

// N8nNodeMapping tells the n8n importer how one n8n node type maps onto a
// mono node type.
//
// Params maps an n8n parameter path to our config key. Paths use dots for
// nested n8n parameters: "options.queryReplacement" is the queryReplacement
// entry of the node's options collection, "table.value" the value of a
// resource-locator parameter. Collection wrappers can be passed through
// as-is: our nodes accept n8n's {"values": [...]} shape for conditions and
// sort, n8n's mapping-mode names (autoMapInputData / defineBelow) for
// data_mode, and {"attribute": [...]} for LDAP attributes.
//
// Operations maps n8n operation values that differ from ours (absent = same
// name). Defaults are config values to add on import because n8n leaves
// parameters at their default value out of its exported JSON, and our
// defaults differ (e.g. n8n's select limit of 50). Trigger types are
// consumers that run through TriggerSubscribers, not NodeExecutors.
type N8nNodeMapping struct {
	MonoType   string
	Params     map[string]string
	Operations map[string]string
	Defaults   map[string]interface{}
	Trigger    bool
}

// sqlV2Params covers Postgres v2 / MySQL v2 / Oracle (resource-mapper based).
var sqlV2Params = map[string]string{
	"operation":                          "operation",
	"query":                              "query",
	"schema.value":                       "schema",
	"table.value":                        "table",
	"columns.mappingMode":                "data_mode",
	"columns.value":                      "values",
	"columns.matchingColumns":            "match_columns",
	"dataMode":                           "data_mode",
	"valuesToSend.values":                "values_to_send",
	"columnToMatchOn":                    "match_columns",
	"valueToMatchOn":                     "value_to_match_on",
	"where":                              "conditions",
	"combineConditions":                  "combine_conditions",
	"sort":                               "sort",
	"limit":                              "limit",
	"returnAll":                          "return_all",
	"deleteCommand":                      "delete_command",
	"restartSequences":                   "restart_sequences",
	"options.cascade":                    "cascade",
	"options.queryBatching":              "query_batching",
	"options.queryReplacement":           "query_replacement",
	"options.outputColumns":              "output_columns",
	"options.skipOnConflict":             "skip_on_conflict",
	"options.replaceEmptyStrings":        "replace_empty_strings",
	"options.stmtBatching":               "query_batching",
	"options.params.values":              "bind_params",
	"options.connectionTimeout":          "",
	"options.largeNumbersOutput":         "",
	"options.nodeVersion":                "",
	"options.detailedOutput":             "",
	"options.selectDistinct":             "",
	"options.priority":                   "",
	"options.autoCommit":                 "",
	"options.delayClosingIdleConnection": "",
	"options.treatQueryParametersInSingleQuotesAsText": "",
}

// sqlV1Params covers the column-list SQL nodes (MSSQL, Snowflake,
// TimescaleDB, CrateDB, QuestDB, Postgres v1): columns is a comma list of
// item fields, updateKey / deleteKey the key column.
var sqlV1Params = map[string]string{
	"operation":                    "operation",
	"query":                        "query",
	"schema":                       "schema",
	"table":                        "table",
	"columns":                      "columns",
	"updateKey":                    "match_columns",
	"deleteKey":                    "match_columns",
	"returnFields":                 "output_columns",
	"additionalFields.mode":        "query_batching",
	"additionalFields.queryParams": "query_params_fields",
	"options.queryReplacement":     "query_replacement",
}

// queueTLSNote: TLS/auth parameters live in the n8n credential, which the
// importer turns into a connection of the platform named in the schema.

// N8nNodeMappings is keyed by the n8n node type.
var N8nNodeMappings = map[string]N8nNodeMapping{
	"n8n-nodes-base.postgres": {MonoType: "db.postgres", Params: sqlV2Params,
		Defaults: map[string]interface{}{"n8n_compat": true, "limit": 50}},
	"n8n-nodes-base.mySql": {MonoType: "db.mysql", Params: sqlV2Params,
		Defaults: map[string]interface{}{"n8n_compat": true, "limit": 50}},
	"n8n-nodes-base.oracleDatabase": {MonoType: "db.oracle", Params: sqlV2Params,
		Defaults: map[string]interface{}{"limit": 50}},
	"n8n-nodes-base.microsoftSql": {MonoType: "db.mssql", Params: sqlV1Params,
		Defaults: map[string]interface{}{"operation": "insert", "data_mode": "auto_map"}},
	"n8n-nodes-base.snowflake": {MonoType: "db.snowflake", Params: sqlV1Params,
		Defaults: map[string]interface{}{"operation": "insert", "data_mode": "auto_map"}},
	"n8n-nodes-base.timescaleDb": {MonoType: "db.timescaledb", Params: sqlV1Params,
		Defaults: map[string]interface{}{"operation": "insert", "schema": "public", "data_mode": "auto_map", "query_batching": "multiple"}},
	"n8n-nodes-base.crateDb": {MonoType: "db.cratedb", Params: sqlV1Params,
		Defaults: map[string]interface{}{"operation": "insert", "schema": "doc", "data_mode": "auto_map", "query_batching": "multiple"}},
	"n8n-nodes-base.questDb": {MonoType: "db.questdb", Params: sqlV1Params,
		Defaults: map[string]interface{}{"operation": "insert", "data_mode": "auto_map", "query_batching": "multiple"}},

	"n8n-nodes-base.mongoDb": {MonoType: "db.mongodb", Params: map[string]string{
		"operation": "operation", "collection": "collection", "query": "query", "queryParameters": "query_parameters",
		"options.limit": "limit", "options.skip": "skip", "options.sort": "sort", "options.projection": "projection",
		"options.sortParameters": "", "options.projectionParameters": "",
		"fields": "fields", "updateKey": "update_key", "upsert": "upsert",
		"options.dateFields": "date_fields", "options.useDotNotation": "use_dot_notation",
		"indexName": "index_name", "indexNameRequired": "index_name", "indexDefinition": "index_definition", "indexType": "index_type",
		"indexDefinitionParameters": "",
	}, Defaults: map[string]interface{}{"operation": "find", "n8n_compat": true, "query": "{}"}},

	"n8n-nodes-base.redis": {MonoType: "db.redis", Params: map[string]string{
		"operation": "operation", "key": "key", "value": "value", "keyType": "key_type", "valueIsJSON": "value_is_json",
		"expire": "expire", "ttl": "ttl", "propertyName": "property_name", "options.dotNotation": "dot_notation",
		"keyPattern": "key_pattern", "getValues": "get_values", "list": "list", "messageData": "message_data",
		"channel": "channel", "tail": "tail",
	}, Defaults: map[string]interface{}{"operation": "info", "n8n_compat": true, "property_name": "propertyName"}},

	"n8n-nodes-base.kafka": {MonoType: "queue.kafka", Params: map[string]string{
		"topic": "topic", "sendInputData": "send_input_data", "message": "message", "useKey": "", "key": "message_key",
		"headersUi.headerValues": "headers", "headerParametersJson": "headers", "jsonParameters": "",
		"options.acks": "acks", "options.compression": "compression", "options.timeout": "timeout",
		"useSchemaRegistry": "", "schemaRegistryUrl": "", "eventName": "",
	}, Defaults: map[string]interface{}{"send_input_data": true}},
	"n8n-nodes-base.rabbitmq": {MonoType: "queue.rabbitmq", Params: map[string]string{
		"operation": "operation", "mode": "mode", "queue": "queue", "exchange": "exchange", "exchangeType": "exchange_type",
		"routingKey": "routing_key", "sendInputData": "send_input_data", "message": "message",
		"options.durable": "durable", "options.autoDelete": "auto_delete", "options.exclusive": "exclusive",
		"options.arguments.argument": "arguments", "options.headers.header": "headers",
		"options.alternateExchange": "alternate_exchange", "options.assertQueue": "assert_queue", "options.assertExchange": "assert_exchange",
	}, Defaults: map[string]interface{}{"mode": "queue", "exchange_type": "fanout", "send_input_data": true}},
	"n8n-nodes-base.amqp": {MonoType: "queue.amqp", Params: map[string]string{
		"sink": "sink", "headerParametersJson": "header_parameters", "options.containerId": "container_id",
		"options.dataAsObject": "data_as_object", "options.sendOnlyProperty": "send_only_property",
		"options.reconnect": "", "options.reconnectLimit": "",
	}},
	"n8n-nodes-base.mqtt": {MonoType: "queue.mqtt", Params: map[string]string{
		"topic": "topic", "sendInputData": "send_input_data", "message": "message", "options.qos": "qos", "options.retain": "retain",
	}, Defaults: map[string]interface{}{"send_input_data": true}},

	"n8n-nodes-base.ldap": {MonoType: "db.ldap", Params: map[string]string{
		"operation": "operation", "dn": "dn", "targetDn": "target_dn", "id": "attribute_id", "value": "value",
		"attributes": "attributes", "baseDN": "base_dn", "searchFor": "search_for", "customFilter": "custom_filter",
		"attribute": "attribute", "searchText": "search_text", "returnAll": "return_all", "limit": "limit",
		"options.attributes": "return_attributes", "options.pageSize": "page_size", "options.scope": "scope",
	}, Defaults: map[string]interface{}{"operation": "search", "limit": 50, "search_for": "(objectclass=*)"}},

	// Triggers: consumers in TriggerSubscribers, wired by the engine.
	"n8n-nodes-base.kafkaTrigger": {MonoType: "trigger.kafka", Trigger: true, Params: map[string]string{
		"topic": "topic", "groupId": "group_id", "options.fromBeginning": "from_beginning",
		"options.jsonParseMessage": "json_parse_message", "options.onlyMessage": "only_message", "options.returnHeaders": "return_headers",
	}, Defaults: map[string]interface{}{"from_beginning": true}},
	"n8n-nodes-base.rabbitmqTrigger": {MonoType: "trigger.rabbitmq", Trigger: true, Params: map[string]string{
		"queue": "queue", "options.binding.bindings": "bindings", "options.jsonParseBody": "json_parse_body",
		"options.onlyContent": "only_content", "options.contentIsBinary": "content_is_binary",
		"options.acknowledge": "acknowledge", "options.parallelMessages": "parallel_messages",
		"options.durable": "durable", "options.autoDelete": "auto_delete", "options.exclusive": "exclusive",
		"options.assertQueue": "assert_queue", "options.arguments.argument": "arguments",
	}, Defaults: map[string]interface{}{"acknowledge": "immediately"}},
	"n8n-nodes-base.amqpTrigger": {MonoType: "trigger.amqp", Trigger: true, Params: map[string]string{
		"sink": "sink", "clientname": "client_name", "subscription": "subscription", "options.containerId": "container_id",
		"options.pullMessagesNumber": "pull_messages_number", "options.jsonParseBody": "json_parse_body", "options.onlyBody": "only_body",
		"options.jsonConvertByteArrayToString": "", "options.parallelProcessing": "", "options.sleepTime": "",
	}},
	"n8n-nodes-base.mqttTrigger": {MonoType: "trigger.mqtt", Trigger: true, Params: map[string]string{
		"topics": "topics", "options.jsonParseBody": "json_parse_body", "options.onlyMessage": "only_message", "options.parallelProcessing": "",
	}},
	"n8n-nodes-base.redisTrigger": {MonoType: "trigger.redis", Trigger: true, Params: map[string]string{
		"channels": "channels", "options.jsonParseBody": "json_parse_body", "options.onlyMessage": "only_message",
	}},
	"n8n-nodes-base.postgresTrigger": {MonoType: "trigger.postgres", Trigger: true, Params: map[string]string{
		"triggerMode": "trigger_mode", "schema.value": "schema", "tableName.value": "table", "firesOn": "fires_on",
		"channelName": "channel_name", "additionalFields.channelName": "channel_name", "additionalFields.functionName": "function_name",
		"additionalFields.triggerName": "trigger_name", "additionalFields.replaceIfExists": "replace_if_exists",
	}, Defaults: map[string]interface{}{"trigger_mode": "createTrigger", "schema": "public", "fires_on": "INSERT"}},
}
