package dbnodes

// credential_platform: redis
//
// RedisNodeSchema documents the config keys RedisNode.Execute reads out of
// its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// This matches the hand-written schemas/db.redis.json field-for-field.
// Note: "key" is marked required here (matching the hand-written schema),
// but Execute only enforces that for most operations — for "keys" it is
// actually optional and defaults to the glob pattern "*" when blank. The
// hand-written schema already carried this same imprecision; a per-operation
// conditional "required" isn't expressible in the tag grammar, so it's
// called out here rather than silently changed.
type RedisNodeSchema struct {
	ConnectionString string `json:"connection_string" schema:"label=Redis Connection String,type=password,required,placeholder=redis://:password@localhost:6379/0"`

	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=get|set|del|exists|expire|hget|hset|hgetall|lpush|lrange|keys|incr|delete|info|llen|pop|publish|push,default=get,help=n8n operations (delete/info/llen/pop/publish/push， or any n8n field below) run once per input item."`

	Key string `json:"key" schema:"label=Key / Pattern,type=text,help=For 'keys'， this is a glob pattern (default '*')."`

	Value string `json:"value" schema:"label=Value,type=text,depends_on_key=operation,depends_on_values=set|hset|lpush"`

	Field string `json:"field" schema:"label=Hash Field,type=text,depends_on_key=operation,depends_on_values=hget|hset"`

	TTLSeconds float64 `json:"ttl_seconds" schema:"label=TTL (seconds),type=number,depends_on_key=operation,depends_on_values=set|expire"`

	Start float64 `json:"start" schema:"label=Start Index,type=number,default=0,depends_on_key=operation,depends_on_values=lrange"`

	Stop float64 `json:"stop" schema:"label=Stop Index,type=number,default=-1,depends_on_key=operation,depends_on_values=lrange"`

	// n8n-compatible mode (see isN8nRedisConfig).
	N8nCompat bool `json:"n8n_compat" schema:"label=n8n Output Format,type=boolean,help=Use n8n's per-item behaviour and output shapes for get/set/keys/incr."`

	PropertyName string `json:"property_name" schema:"label=Property Name,type=text,placeholder=propertyName,depends_on_key=operation,depends_on_values=get|pop"`

	KeyType string `json:"key_type" schema:"label=Key Type,type=select,options=automatic|string|hash|list|sets,depends_on_key=operation,depends_on_values=get|set"`

	ValueIsJSON bool `json:"value_is_json" schema:"label=Value Is JSON,type=boolean,depends_on_key=key_type,depends_on_values=hash"`

	Expire bool `json:"expire" schema:"label=Expire,type=boolean,depends_on_key=operation,depends_on_values=set|incr"`

	TTL float64 `json:"ttl" schema:"label=TTL (seconds),type=number,depends_on_key=expire,depends_on_values=true"`

	KeyPattern string `json:"key_pattern" schema:"label=Key Pattern,type=text,placeholder=user:*,depends_on_key=operation,depends_on_values=keys"`

	GetValues bool `json:"get_values" schema:"label=Get Values,type=boolean,help=Return each key with its value (default on in n8n mode).,depends_on_key=operation,depends_on_values=keys"`

	List string `json:"list" schema:"label=List,type=text,depends_on_key=operation,depends_on_values=llen|push|pop"`

	Channel string `json:"channel" schema:"label=Channel,type=text,depends_on_key=operation,depends_on_values=publish"`

	MessageData string `json:"message_data" schema:"label=Data,type=textarea,rows=3,depends_on_key=operation,depends_on_values=publish|push"`

	Tail bool `json:"tail" schema:"label=Tail,type=boolean,help=Push to / pop from the end of the list.,depends_on_key=operation,depends_on_values=push|pop"`

	DotNotation bool `json:"dot_notation" schema:"label=Dot Notation,type=boolean,help=Treat a.b in Property Name as nested (default on).,depends_on_key=operation,depends_on_values=get|pop"`
}
