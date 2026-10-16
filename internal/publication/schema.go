package publication

// EntrySchema exposes only publication fields, never credentials or arbitrary config.
func EntrySchema() map[string]interface{} {
	props := map[string]interface{}{}
	for key, desc := range map[string]string{
		"platform": "Platform or destination name", "kind": "post, comment, reply, article, video or other publication kind",
		"title": "Published title", "body": "Exact published content", "url": "Live publication URL", "remote_id": "Destination's publication ID",
		"parent_url": "Parent post or comment URL/ID", "account": "Publishing account label", "workflow_id": "Source workflow ID", "execution_id": "Source execution ID",
		"node_id": "Source node ID", "agent_id": "Publishing agent ID", "org_id": "Source organization ID", "role_id": "Source role ID",
		"published_at": "Publication timestamp (RFC3339)", "idempotency_key": "Stable key for registering the same publication again",
	} {
		props[key] = map[string]interface{}{"type": "string", "description": desc}
	}
	props["media"] = map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Published media references"}
	return props
}

// RegisterSchema is EntrySchema with approval: what an agent or the chat
// registers belongs to a send the user approved and that was sent, named by
// its id while the user's approval of sends is on.
func RegisterSchema() map[string]interface{} {
	props := EntrySchema()
	props["approval"] = map[string]interface{}{"type": "string", "description": "The id of the send the user approved, and that was sent, which this publication belongs to: needed while the user's approval of sends is on"}
	return props
}

func FilterSchema() map[string]interface{} {
	props := map[string]interface{}{}
	for _, key := range []string{"search", "platform", "kind", "workflow_id", "agent_id", "since", "until"} {
		props[key] = map[string]interface{}{"type": "string", "description": key + " filter (dates in RFC3339 or YYYY-MM-DD)"}
	}
	props["limit"] = map[string]interface{}{"type": "integer", "description": "Page size (default 50, maximum 1000)"}
	props["offset"] = map[string]interface{}{"type": "integer", "description": "Nonnegative page offset"}
	return props
}
