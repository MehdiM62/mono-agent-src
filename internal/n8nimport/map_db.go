package n8nimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.postgres", func(c *MapContext) Mapping { return mapSQL(c, "db.postgres", "postgresql", "postgres") })
	Register("n8n-nodes-base.mySql", func(c *MapContext) Mapping { return mapSQL(c, "db.mysql", "mysql", "mySql") })
	Register("n8n-nodes-base.mongoDb", mapMongo)
	Register("n8n-nodes-base.redis", mapRedis)
}

// queryParams splits n8n's queryReplacement ("{{ $json.a }},{{ $json.b }}"
// or "a,b") into one translated parameter per placeholder.
func queryParams(c *MapContext, param string) []interface{} {
	raw, _ := c.Raw(param).(string)
	expr := strings.HasPrefix(raw, "=")
	body := strings.TrimPrefix(raw, "=")
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch {
		case strings.HasPrefix(body[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(body[i:], "}}"):
			depth--
			i++
		case body[i] == ',' && depth == 0:
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	parts = append(parts, body[start:])
	var out []interface{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if expr {
			p = "=" + p
		}
		out = append(out, c.Translate(param, p))
	}
	return out
}

// sqlQuote renders a value template as a quoted SQL literal.
func sqlQuote(v interface{}) string {
	return "'" + strings.ReplaceAll(fmt.Sprint(v), "'", "''") + "'"
}

func mapSQL(c *MapContext, monoType, platform, credType string) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential(platform, credType)}
	c.Ignore("resource", "options.queryBatching", "options.connectionTimeout", "options.connectionTimeoutMillis",
		"options.largeNumbersOutput", "options.outputColumns", "options.delayClosingIdleConnection", "options.decimalNumbers")
	op := c.Literal("operation", "insert")
	table := c.Str("table", "")
	if schema := c.Str("schema", "public"); schema != "public" && schema != "" {
		table = schema + "." + table
	}
	switch op {
	case "executeQuery":
		cfg["operation"] = "execute"
		cfg["query"] = c.Str("query", "")
		if c.Has("options.queryReplacement") {
			cfg["params"] = queryParams(c, "options.queryReplacement")
		}
		if c.Has("additionalFields.queryParams") {
			cfg["params"] = queryParams(c, "additionalFields.queryParams")
		}
		c.Ignore("additionalFields.mode")
		return Mapping{Type: monoType, Config: cfg}
	case "insert", "upsert", "update":
		cfg["operation"] = map[string]string{"insert": "insert", "upsert": "insert", "update": "update"}[op]
		cfg["table"] = table
		data, match := sqlData(c)
		cfg["data"] = data
		if op != "insert" {
			var conds []string
			for _, col := range match {
				conds = append(conds, fmt.Sprintf("%s = %s", col, sqlQuote(c.Translate("columns", "={{ $json."+col+" }}"))))
			}
			cfg["where"] = strings.Join(conds, " AND ")
		}
		if op == "upsert" {
			c.Warn("operation", "upsert", "becomes an insert; conflicting rows make the node fail")
		}
		c.Ignore("options.skipOnConflict", "options.replaceEmptyStrings", "returnFields", "additionalFields.mode")
	case "select":
		cfg["operation"] = "select"
		cfg["table"] = table
		var conds []string
		for i, w := range c.paramList("where.values") {
			cond := fmt.Sprint(w["condition"])
			if cond == "<nil>" || cond == "equal" {
				cond = "="
			}
			if strings.HasPrefix(cond, "IS ") {
				conds = append(conds, fmt.Sprintf("%v %s", w["column"], cond))
				continue
			}
			conds = append(conds, fmt.Sprintf("%v %s %s", w["column"], cond, sqlQuote(c.Translate(fmt.Sprintf("where[%d]", i), w["value"]))))
		}
		join := " " + c.Literal("combineConditions", "AND") + " "
		cfg["where"] = strings.Join(conds, join)
		if !c.Bool("returnAll", false) {
			c.Warn("limit", c.Raw("limit"), "select returns every matching row; add LIMIT with an execute query if needed")
			c.Ignore("limit")
		}
		if c.Has("sort") {
			c.Unsupported("sort")
		}
	case "deleteTable":
		switch cmd := c.Literal("deleteCommand", "truncate"); cmd {
		case "delete":
			cfg["operation"] = "delete"
			cfg["table"] = table
			c.Warn("where", c.Raw("where"), "conditions are not converted; set where before running")
			c.Ignore("combineConditions")
		default:
			cfg["operation"] = "execute"
			cfg["query"] = strings.ToUpper(cmd) + " TABLE " + table
			c.Ignore("restartSequences", "options.cascade")
		}
	default:
		return c.OperationUnsupported("", op)
	}
	return Mapping{Type: monoType, Config: cfg}
}

// sqlData returns the row to write and the columns that identify it.
func sqlData(c *MapContext) (interface{}, []string) {
	var match []string
	if c.version(2) {
		if l, ok := c.Raw("columns.matchingColumns").([]interface{}); ok {
			for _, x := range l {
				match = append(match, fmt.Sprint(x))
			}
		}
		if col := c.Literal("columnToMatchOn", ""); col != "" {
			match = append(match, col)
			c.Ignore("valueToMatchOn")
		}
		if c.Has("columns") {
			return columnsValue(c, "columns"), match
		}
		if c.Literal("dataMode", "autoMapInputData") == "defineBelow" {
			data := map[string]interface{}{}
			for i, v := range c.paramList("valuesToSend.values") {
				data[fmt.Sprint(v["column"])] = c.Translate(fmt.Sprintf("valuesToSend[%d]", i), v["value"])
			}
			return data, match
		}
		return "{{ json $json }}", match
	}
	data := map[string]interface{}{}
	for _, col := range commaList(c.Literal("columns", "")) {
		col = strings.TrimSuffix(strings.Split(col, ":")[0], " ")
		data[col] = "{{ index $json " + fmt.Sprintf("%q", col) + " }}"
	}
	if key := c.Literal("updateKey", "id"); key != "" {
		match = append(match, key)
	}
	return data, match
}

// staticJSON parses a JSON-text parameter, else translates it.
func staticJSON(c *MapContext, param, def string) interface{} {
	raw, _ := c.Raw(param).(string)
	if raw == "" {
		raw = def
	}
	if !strings.HasPrefix(raw, "=") {
		var v interface{}
		if json.Unmarshal([]byte(raw), &v) == nil {
			return escapeDeep(v)
		}
	}
	return c.Translate(param, raw)
}

func mapMongo(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("mongodb", "mongoDb"), "collection": c.Str("collection", "")}
	c.Ignore("resource", "options.dateFields", "options.useDotNotation")
	c.Note("the database comes from the mongodb connection")
	fieldsDoc := func() interface{} {
		fields := commaList(c.Literal("fields", ""))
		if len(fields) == 0 {
			return "{{ json $json }}"
		}
		doc := map[string]interface{}{}
		for _, f := range fields {
			doc[f] = "{{ index $json " + fmt.Sprintf("%q", f) + " }}"
		}
		return doc
	}
	switch op := c.Literal("operation", "find"); op {
	case "find":
		cfg["operation"] = "find"
		cfg["filter"] = staticJSON(c, "query", "{}")
		if c.Has("options.limit") {
			cfg["limit"] = c.Num("options.limit", 0)
		}
		for _, o := range []string{"options.skip", "options.sort", "options.projection"} {
			if c.Has(o) {
				c.Unsupported(o)
			}
		}
	case "insert":
		cfg["operation"] = "insert_one"
		cfg["document"] = fieldsDoc()
	case "update", "findOneAndUpdate", "findOneAndReplace":
		key := c.Literal("updateKey", "id")
		cfg["operation"] = "update_one"
		cfg["filter"] = map[string]interface{}{key: "{{ index $json " + fmt.Sprintf("%q", key) + " }}"}
		cfg["update"] = map[string]interface{}{"$set": fieldsDoc()}
		if c.Bool("upsert", false) {
			c.Unsupported("upsert")
		}
		if op != "update" {
			c.Note("%s became update_one ($set of the listed fields)", op)
		}
	case "delete":
		cfg["operation"] = "delete_many"
		cfg["filter"] = staticJSON(c, "query", "{}")
	case "aggregate":
		cfg["operation"] = "aggregate"
		cfg["pipeline"] = staticJSON(c, "query", "[]")
	default:
		return c.OperationUnsupported("", op)
	}
	return Mapping{Type: "db.mongodb", Config: cfg}
}

func mapRedis(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("redis", "redis")}
	c.Ignore("options.dotNotation")
	op := c.Literal("operation", "info")
	switch op {
	case "get":
		cfg["key"] = c.Str("key", "")
		switch kt := c.Literal("keyType", "automatic"); kt {
		case "hash":
			cfg["operation"] = "hgetall"
		case "list":
			cfg["operation"], cfg["start"], cfg["stop"] = "lrange", 0.0, -1.0
		case "sets":
			return c.OperationUnsupported("", "get sets")
		default:
			cfg["operation"] = "get"
		}
		if p := c.Literal("propertyName", "propertyName"); p != "" {
			c.Note("the value is returned in mono's redis output field, not in %q", p)
		}
	case "set":
		cfg["key"] = c.Str("key", "")
		cfg["value"] = c.Str("value", "")
		cfg["operation"] = "set"
		if kt := c.Literal("keyType", "automatic"); kt == "hash" || kt == "list" || kt == "sets" {
			return c.OperationUnsupported("", "set "+kt)
		}
		if c.Bool("expire", false) {
			cfg["ttl_seconds"] = c.Num("ttl", 60)
		} else {
			c.Ignore("ttl")
		}
		c.Ignore("valueIsJSON")
	case "delete":
		cfg["operation"], cfg["key"] = "del", c.Str("key", "")
	case "incr":
		cfg["operation"], cfg["key"] = "incr", c.Str("key", "")
		if c.Bool("expire", false) {
			c.Warn("expire", true, "incr does not set an expiry in mono-agent")
			c.Ignore("ttl")
		}
	case "keys":
		cfg["operation"], cfg["key"] = "keys", c.Str("keyPattern", "*")
		c.Ignore("getValues")
	case "push":
		cfg["operation"], cfg["key"], cfg["value"] = "lpush", c.Str("list", ""), c.Str("messageData", "")
		if c.Bool("tail", false) {
			c.Warn("tail", true, "values are pushed to the head of the list")
		}
	default:
		return c.OperationUnsupported("", op)
	}
	return Mapping{Type: "db.redis", Config: cfg}
}
