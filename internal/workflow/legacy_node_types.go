package workflow

// legacyNodeTypes maps the node types saved workflows may still name to the
// type that runs them: the unprefixed ids of early workflows, and the comm.*
// platforms that used to exist twice, once as comm.* and once as service.*,
// whose service.* node now runs the old configs unchanged. Registries alias
// them (LegacyNodeTypes); CanonicalNodeType resolves them where there is no
// registry at hand, as a node is classified as a send (internal/hilsend).
var legacyNodeTypes = map[string]string{
	"google_sheets": "service.google_sheets", "gmail": "service.gmail", "google_drive": "service.google_drive",
	"github": "service.github", "notion": "service.notion", "airtable": "service.airtable",
	"jira": "service.jira", "linear": "service.linear", "asana": "service.asana",
	"stripe": "service.stripe", "shopify": "service.shopify", "salesforce": "service.salesforce",
	"hubspot": "service.hubspot",
	"slack":   "comm.slack", "discord": "service.discord", "telegram": "comm.telegram",
	"twilio": "comm.twilio", "whatsapp": "comm.whatsapp",
	"email_send": "comm.email_send", "email_read": "comm.email_read",
	"mysql": "db.mysql", "postgres": "db.postgres", "mongodb": "db.mongodb", "redis": "db.redis",
	"datetime": "data.datetime", "crypto": "data.crypto", "html": "data.html",
	"xml": "data.xml", "markdown": "data.markdown", "spreadsheet": "data.spreadsheet",
	"compression": "data.compression", "write_binary_file": "data.write_binary_file",
	"if": "core.if", "switch": "core.switch", "merge": "core.merge", "set": "core.set",
	"code": "core.code", "filter": "core.filter", "sort": "core.sort", "limit": "core.limit",
	"aggregate": "core.aggregate", "wait": "core.wait",
	"http_request": "http.request", "http_response": "http.response",
	"execute_command": "system.execute_command", "rss_read": "system.rss_read",
	"read_write_file": "system.read_write_file",

	"comm.bluesky":      "service.bluesky",
	"comm.mastodon":     "service.mastodon",
	"comm.reddit":       "service.reddit",
	"comm.discord":      "service.discord",
	"comm.outlook_read": "service.outlook_mail",
	"comm.outlook_send": "service.outlook_mail",
}

// LegacyNodeTypes returns a copy of the legacy name → canonical type map.
func LegacyNodeTypes() map[string]string {
	out := make(map[string]string, len(legacyNodeTypes))
	for from, to := range legacyNodeTypes {
		out[from] = to
	}
	return out
}

// CanonicalNodeType is the type that runs nodeType: a legacy name's
// canonical type, else nodeType itself.
func CanonicalNodeType(nodeType string) string {
	if canonical, ok := legacyNodeTypes[nodeType]; ok {
		return canonical
	}
	return nodeType
}
