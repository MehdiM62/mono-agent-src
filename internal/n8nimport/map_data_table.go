package n8nimport

import "fmt"

func init() {
	Register("n8n-nodes-base.dataTable", mapDataTable)
}

// mapDataTable maps n8n's Data table node to core.data_table, which keeps
// the same tables in the profile: the operations, the resource-mapper
// columns, the conditions and the table by name.
func mapDataTable(c *MapContext) Mapping {
	res := c.Literal("resource", "row")
	def := "insert"
	if res == "table" {
		def = "list"
	}
	op := c.Literal("operation", def)
	cfg := map[string]interface{}{"resource": res, "operation": op}
	c.Ignore("clearWarning", "deleteWarning", "notice")
	if res == "table" && op == "create" {
		cfg["table"] = c.Str("tableName", "")
	} else if res == "table" && op == "list" {
		c.Ignore("dataTableId")
	} else {
		cfg["table"] = dataTableRef(c)
	}
	switch res + "." + op {
	case "row.insert", "row.update", "row.upsert":
		if c.Literal("columns.mappingMode", "defineBelow") == "autoMapInputData" {
			cfg["columns_mode"] = "auto"
		} else if v, _ := c.Raw("columns.value").(map[string]interface{}); len(v) > 0 {
			cfg["values"] = c.Translate("columns", v)
		}
		c.Ignore("columns")
	case "table.create":
		var cols []interface{}
		for _, col := range c.paramList("columns.column") {
			typ, _ := col["type"].(string)
			if typ == "" {
				typ = "string"
			}
			cols = append(cols, map[string]interface{}{"name": fmt.Sprint(col["name"]), "type": typ})
		}
		c.Ignore("columns")
		if cols != nil {
			cfg["columns"] = cols
		}
		cfg["reuse_existing"] = c.Bool("options.createIfNotExists", true)
	case "table.list":
		cfg["return_all"] = c.Bool("returnAll", true)
		cfg["limit"] = c.Num("limit", 50)
		if f := c.Str("options.filterName", ""); f != "" {
			cfg["filter_name"] = f
		}
		cfg["sort_field"] = c.Literal("options.sortField", "name")
		cfg["sort_direction"] = c.Literal("options.sortDirection", "asc")
	case "table.update":
		cfg["new_name"] = c.Str("newName", "")
	}
	if res == "row" && op != "insert" {
		cfg["match"] = map[string]string{"allConditions": "all"}[c.Literal("matchType", "anyCondition")]
		if cfg["match"] == "" {
			cfg["match"] = "any"
		}
		var filters []interface{}
		for _, f := range c.paramList("filters.conditions") {
			cond, _ := f["condition"].(string)
			if cond == "" {
				cond = "eq"
			}
			m := map[string]interface{}{"column": fmt.Sprint(f["keyName"]), "condition": cond}
			if v, ok := f["keyValue"]; ok {
				m["value"] = c.Translate("filters", v)
			}
			filters = append(filters, m)
		}
		c.Ignore("filters")
		if filters != nil {
			cfg["filters"] = filters
		}
	}
	if op == "get" {
		cfg["return_all"] = c.Bool("returnAll", false)
		cfg["limit"] = c.Num("limit", 50)
		if c.Bool("orderBy", false) {
			cfg["order_by"] = c.Str("orderByColumn", "createdAt")
			cfg["order_direction"] = c.Literal("orderByDirection", "DESC")
		}
		c.Ignore("orderByColumn", "orderByDirection")
	}
	if c.Has("options.dryRun") {
		cfg["dry_run"] = c.Bool("options.dryRun", false)
	}
	if c.Has("options.optimizeBulk") {
		cfg["optimize_bulk"] = c.Bool("options.optimizeBulk", false)
	}
	return Mapping{Type: "core.data_table", Config: cfg}
}

// dataTableRef is the table of a dataTableId resource locator: its name
// (typed, or the name n8n cached for a table picked from the list), else
// the n8n table id, which the user has to replace.
func dataTableRef(c *MapContext) interface{} {
	v, ok := c.Raw("dataTableId").(map[string]interface{})
	if !ok {
		return c.Str("dataTableId", "")
	}
	if v["mode"] == "name" {
		return c.Translate("dataTableId", v["value"])
	}
	if name, _ := v["cachedResultName"].(string); name != "" {
		c.Note("the data table is found by its n8n name %q: create it with monoagentcli table create (or a core.data_table create step)", name)
		return name
	}
	id := c.Translate("dataTableId", v["value"])
	if id != nil && id != "" {
		c.Warn("dataTableId", v["value"], "is an n8n data table ID: set the name or id of a mono-agent data table (monoagentcli table list)")
	}
	return id
}
