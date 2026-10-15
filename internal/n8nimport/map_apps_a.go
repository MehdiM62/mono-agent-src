package n8nimport

import (
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.github", mapGithub)
	Register("n8n-nodes-base.notion", mapNotion)
	Register("n8n-nodes-base.airtable", mapAirtable)
	Register("n8n-nodes-base.linear", mapLinear)
	Register("n8n-nodes-base.jira", mapJira)
	Register("n8n-nodes-base.asana", mapAsana)
}

// copyFields maps n8n collection keys onto mono config keys.
func copyFields(c *MapContext, cfg map[string]interface{}, coll string, names map[string]string) {
	for from, to := range names {
		if p := coll + "." + from; c.Has(p) {
			cfg[to] = c.Value(p)
		}
	}
}

func mapGithub(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("github", "githubApi", "githubOAuth2Api")}
	c.Ignore("authentication")
	res, op := resOp(c, "issue", "create", map[string]string{"repository": "getIssues", "user": "getRepositories", "workflow": "dispatch", "review": "create"})
	repo := func() {
		cfg["owner"] = c.Str("owner", "")
		cfg["repo"] = c.Str("repository", "")
	}
	switch res + "." + op {
	case "issue.create":
		repo()
		cfg["operation"] = "create_issue"
		cfg["title"] = c.Str("title", "")
		cfg["body"] = c.Str("body", "")
		for _, p := range []string{"labels", "assignees"} {
			if len(c.paramList(p)) > 0 {
				c.Unsupported(p)
			}
		}
	case "issue.get":
		repo()
		cfg["operation"] = "get_issue"
		cfg["number"] = c.Num("issueNumber", 0)
	case "issue.edit":
		repo()
		cfg["operation"] = "update_issue"
		cfg["number"] = c.Num("issueNumber", 0)
		copyFields(c, cfg, "editFields", map[string]string{"title": "title", "body": "body"})
	case "repository.getIssues":
		repo()
		cfg["operation"] = "list_issues"
		c.Ignore("returnAll", "limit")
	case "repository.getPullRequests":
		repo()
		cfg["operation"] = "list_prs"
		c.Ignore("returnAll", "limit")
	case "pullRequest.create":
		repo()
		cfg["operation"] = "create_pr"
		for _, k := range []string{"title", "body", "head", "base"} {
			cfg[k] = c.Str(k, "")
		}
	case "release.create":
		repo()
		cfg["operation"] = "create_release"
		cfg["tag_name"] = c.Str("releaseTag", "")
		copyFields(c, cfg, "additionalFields", map[string]string{"name": "release_name", "body": "body"})
	case "release.getAll":
		repo()
		cfg["operation"] = "list_releases"
		c.Ignore("returnAll", "limit")
	case "user.getRepositories":
		cfg["operation"] = "list_repos"
		c.Ignore("returnAll", "limit", "owner")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.github", Config: cfg}
}

// notionProperty converts one n8n propertiesUi entry into a Notion API
// property value.
func notionProperty(c *MapContext, i int, p map[string]interface{}) (string, interface{}, bool) {
	key := fmt.Sprint(p["key"])
	name, typ, _ := strings.Cut(key, "|")
	if t, ok := p["type"].(string); ok && t != "" {
		typ = t
	}
	param := fmt.Sprintf("propertiesUi[%d]", i)
	tr := func(k string) interface{} { return c.Translate(param+"."+k, p[k]) }
	text := func(v interface{}) interface{} {
		return []interface{}{map[string]interface{}{"text": map[string]interface{}{"content": v}}}
	}
	switch typ {
	case "title":
		return name, map[string]interface{}{"title": text(tr("title"))}, true
	case "rich_text":
		return name, map[string]interface{}{"rich_text": text(tr("textContent"))}, true
	case "number":
		return name, map[string]interface{}{"number": tr("numberValue")}, true
	case "checkbox":
		return name, map[string]interface{}{"checkbox": tr("checkboxValue")}, true
	case "select", "status":
		return name, map[string]interface{}{typ: map[string]interface{}{"name": tr(typ + "Value")}}, true
	case "multi_select":
		var opts []interface{}
		if l, ok := p["multiSelectValue"].([]interface{}); ok {
			for _, o := range l {
				opts = append(opts, map[string]interface{}{"name": c.Translate(param, o)})
			}
		}
		return name, map[string]interface{}{"multi_select": opts}, true
	case "url", "email", "phone_number":
		k := map[string]string{"url": "urlValue", "email": "emailValue", "phone_number": "phoneValue"}[typ]
		return name, map[string]interface{}{typ: tr(k)}, true
	case "date":
		return name, map[string]interface{}{"date": map[string]interface{}{"start": tr("date")}}, true
	}
	c.Warn(param, key, "Notion property type %q is not converted", typ)
	return "", nil, false
}

func notionProperties(c *MapContext) map[string]interface{} {
	props := map[string]interface{}{}
	for i, p := range c.paramList("propertiesUi.propertyValues") {
		if k, v, ok := notionProperty(c, i, p); ok {
			props[k] = v
		}
	}
	return props
}

// notionBlocks converts blockUi entries into Notion paragraph-style blocks.
func notionBlocks(c *MapContext) []interface{} {
	var out []interface{}
	for i, b := range c.paramList("blockUi.blockValues") {
		typ, _ := b["type"].(string)
		if typ == "" {
			typ = "paragraph"
		}
		content := c.Translate(fmt.Sprintf("blockUi[%d].textContent", i), b["textContent"])
		out = append(out, map[string]interface{}{"object": "block", "type": typ,
			typ: map[string]interface{}{"rich_text": []interface{}{map[string]interface{}{"text": map[string]interface{}{"content": content}}}}})
	}
	return out
}

func mapNotion(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("notion", "notionApi", "notionOAuth2Api")}
	c.Ignore("authentication", "simple", "options.iconType", "options.icon")
	res, op := resOp(c, "page", "create", map[string]string{"database": "get", "block": "append", "user": "get", "databasePage": "create"})
	switch res + "." + op {
	case "databasePage.create":
		cfg["operation"] = "create_page"
		cfg["database_id"] = c.Str("databaseId", "")
		props := notionProperties(c)
		if c.Has("title") {
			props["Name"] = map[string]interface{}{"title": []interface{}{map[string]interface{}{"text": map[string]interface{}{"content": c.Str("title", "")}}}}
		}
		cfg["properties"] = props
		if b := notionBlocks(c); len(b) > 0 {
			cfg["children"] = b
		}
	case "databasePage.update":
		cfg["operation"] = "update_page"
		cfg["page_id"] = c.Str("pageId", "")
		cfg["properties"] = notionProperties(c)
	case "databasePage.get", "page.get":
		cfg["operation"] = "get_page"
		cfg["page_id"] = c.Str("pageId", "")
	case "databasePage.getAll":
		cfg["operation"] = "query_database"
		cfg["database_id"] = c.Str("databaseId", "")
		if !c.Bool("returnAll", false) {
			cfg["page_size"] = c.Float("limit", 50)
		}
		switch c.Literal("filterType", "none") {
		case "json":
			cfg["filter"] = c.jsonParam("filterJson")
		case "manual":
			c.Warn("filters", c.Raw("filters"), "manual filters are not converted; write them as Notion filter JSON")
			c.Ignore("matchType")
		}
		c.Ignore("options.sort", "options.downloadFiles")
	case "page.create":
		cfg["operation"] = "create_page"
		cfg["parent_id"] = c.Str("pageId", "")
		cfg["properties"] = map[string]interface{}{"title": map[string]interface{}{"title": []interface{}{map[string]interface{}{"text": map[string]interface{}{"content": c.Str("title", "")}}}}}
		if b := notionBlocks(c); len(b) > 0 {
			cfg["children"] = b
		}
	case "block.append":
		cfg["operation"] = "append_blocks"
		cfg["page_id"] = c.Str("blockId", "")
		cfg["children"] = notionBlocks(c)
	case "database.get":
		cfg["operation"] = "get_database"
		cfg["database_id"] = c.Str("databaseId", "")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.notion", Config: cfg}
}

// columnsValue returns the fields of a resource-mapper parameter: the
// whole item for autoMapInputData, else the defined values.
func columnsValue(c *MapContext, param string) interface{} {
	if c.Literal(param+".mappingMode", "defineBelow") == "autoMapInputData" {
		c.Ignore(param)
		return "{{ json $json }}"
	}
	v, _ := c.Raw(param + ".value").(map[string]interface{})
	c.Ignore(param)
	return c.Translate(param, v)
}

func mapAirtable(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("airtable", "airtableTokenApi", "airtableOAuth2Api", "airtableApi")}
	c.Ignore("authentication", "options.typecast", "options.ignoreFields", "options.bulkSize")
	if !c.version(2) {
		cfg["base_id"] = c.Str("application", "")
		cfg["table"] = c.Str("table", "")
		switch op := c.Literal("operation", "read"); op {
		case "append":
			cfg["operation"] = "create_record"
			cfg["fields"] = "{{ json $json }}"
			c.Ignore("addAllFields", "fields")
		case "list":
			cfg["operation"] = "list_records"
			cfg["filter_formula"] = c.Str("additionalOptions.filterByFormula", "")
			cfg["view"] = c.Str("additionalOptions.view", "")
			c.Ignore("returnAll", "limit", "downloadAttachments", "downloadFieldNames")
		case "read":
			cfg["operation"] = "get_record"
			cfg["record_id"] = c.Str("id", "")
		case "update":
			cfg["operation"] = "update_record"
			cfg["record_id"] = c.Str("id", "")
			cfg["fields"] = "{{ json $json }}"
			c.Ignore("updateAllFields", "fields")
		case "delete":
			cfg["operation"] = "delete_record"
			cfg["record_id"] = c.Str("id", "")
		default:
			return c.OperationUnsupported("", op)
		}
		return Mapping{Type: "service.airtable", Config: cfg}
	}
	cfg["base_id"] = c.Str("base", "")
	cfg["table"] = c.Str("table", "")
	res, op := resOp(c, "record", "get", map[string]string{"base": "getMany"})
	switch res + "." + op {
	case "record.create":
		cfg["operation"] = "create_record"
		cfg["fields"] = columnsValue(c, "columns")
	case "record.update", "record.upsert":
		cfg["operation"] = "update_record"
		if v, ok := c.Raw("columns.value").(map[string]interface{}); ok && v["id"] != nil {
			cfg["record_id"] = c.Translate("columns.value.id", v["id"])
		}
		cfg["fields"] = columnsValue(c, "columns")
		if op == "upsert" {
			c.Warn("operation", "upsert", "updates the record with the given id; it does not create missing records")
		}
		c.Ignore("options.updateAllMatches")
	case "record.search":
		cfg["operation"] = "list_records"
		cfg["filter_formula"] = c.Str("filterByFormula", "")
		cfg["view"] = c.Str("options.view", "")
		c.Ignore("returnAll", "limit", "options.fields", "sort", "options.downloadFields")
	case "record.get":
		cfg["operation"] = "get_record"
		cfg["record_id"] = c.Str("id", "")
		c.Ignore("options.downloadFields")
	case "record.deleteRecord":
		cfg["operation"] = "delete_record"
		cfg["record_id"] = c.Str("id", "")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.airtable", Config: cfg}
}

func mapLinear(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("linear", "linearApi", "linearOAuth2Api")}
	c.Ignore("authentication")
	res, op := resOp(c, "issue", "create", map[string]string{"comment": "addComment"})
	fields := map[string]string{"description": "description", "priorityId": "priority", "stateId": "state_id", "title": "title", "teamId": "team_id"}
	switch res + "." + op {
	case "issue.create":
		cfg["operation"] = "create_issue"
		cfg["team_id"] = c.Str("teamId", "")
		cfg["title"] = c.Str("title", "")
		copyFields(c, cfg, "additionalFields", fields)
	case "issue.update":
		cfg["operation"] = "update_issue"
		cfg["issue_id"] = c.Str("issueId", "")
		copyFields(c, cfg, "updateFields", fields)
	case "issue.get":
		cfg["operation"] = "get_issue"
		cfg["issue_id"] = c.Str("issueId", "")
	case "issue.getAll":
		cfg["operation"] = "list_issues"
		c.Ignore("returnAll", "limit")
	default:
		return c.OperationUnsupported(res, op)
	}
	if p, ok := cfg["priority"].(float64); ok {
		cfg["priority"] = fmt.Sprint(p)
	}
	return Mapping{Type: "service.linear", Config: cfg}
}

func mapJira(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("jira")}
	c.Ignore("jiraVersion")
	res, op := resOp(c, "issue", "create", map[string]string{"issueComment": "add", "issueAttachment": "add", "user": "create"})
	switch res + "." + op {
	case "issue.create":
		cfg["operation"] = "create_issue"
		cfg["project_key"] = c.Str("project", "")
		cfg["issue_type"] = c.Str("issueType", "Task")
		cfg["summary"] = c.Str("summary", "")
		copyFields(c, cfg, "additionalFields", map[string]string{"description": "description"})
		c.Note("n8n stores the project and issue type as ids; check project_key and issue_type hold the key and type name")
	case "issue.update":
		cfg["operation"] = "update_issue"
		cfg["issue_key"] = c.Str("issueKey", "")
		copyFields(c, cfg, "updateFields", map[string]string{"description": "description", "summary": "summary"})
	case "issue.get":
		cfg["operation"] = "get_issue"
		cfg["issue_key"] = c.Str("issueKey", "")
		c.Ignore("simplifyOutput", "additionalFields")
	case "issue.getAll":
		cfg["operation"] = "list_issues"
		cfg["jql"] = c.Str("options.jql", "")
		c.Ignore("returnAll", "limit", "options.fields", "options.expand", "options.fieldsByKey")
	case "issueComment.add":
		cfg["operation"] = "add_comment"
		cfg["issue_key"] = c.Str("issueKey", "")
		cfg["comment"] = c.Str("comment", "")
		c.Ignore("jsonParameters", "options.wikiMarkup")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.jira", Config: cfg}
}

func mapAsana(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("asana", "asanaApi", "asanaOAuth2Api")}
	c.Ignore("authentication")
	res, op := resOp(c, "task", "create", map[string]string{"project": "get", "user": "get", "taskComment": "add", "subtask": "create"})
	props := map[string]string{"notes": "notes", "due_on": "due_on", "assignee": "assignee", "completed": "completed", "name": "name"}
	switch res + "." + op {
	case "task.create":
		cfg["operation"] = "create_task"
		cfg["workspace_gid"] = c.Str("workspace", "")
		cfg["name"] = c.Str("name", "")
		copyFields(c, cfg, "otherProperties", props)
		if p, ok := c.Raw("otherProperties.projects").([]interface{}); ok && len(p) > 0 {
			cfg["project_gid"] = c.Translate("otherProperties.projects", p[0])
		}
	case "task.update":
		cfg["operation"] = "update_task"
		cfg["task_gid"] = c.Str("id", "")
		copyFields(c, cfg, "otherProperties", props)
	case "task.get":
		cfg["operation"] = "get_task"
		cfg["task_gid"] = c.Str("id", "")
	case "task.getAll":
		cfg["operation"] = "list_tasks"
		copyFields(c, cfg, "filters", map[string]string{"workspace": "workspace_gid", "project": "project_gid", "assignee": "assignee"})
		c.Ignore("returnAll", "limit")
	case "project.getAll":
		cfg["operation"] = "list_projects"
		cfg["workspace_gid"] = c.Str("workspace", "")
		c.Ignore("returnAll", "limit")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.asana", Config: cfg}
}
