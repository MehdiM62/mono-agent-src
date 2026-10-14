package n8nimport

import (
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.postgres", func(c *MapContext) Mapping { return mapSQL(c, "db.postgres", "postgresql", "postgres") })
	Register("n8n-nodes-base.mySql", func(c *MapContext) Mapping { return mapSQL(c, "db.mysql", "mysql", "mySql") })
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
