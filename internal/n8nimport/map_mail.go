package n8nimport

import (
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.gmail", mapGmail)
	Register("n8n-nodes-base.microsoftOutlook", mapOutlook)
	Register("n8n-nodes-base.googleSheets", mapGoogleSheets)
	Register("n8n-nodes-base.googleDrive", mapGoogleDrive)
}

func mapGmail(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("gmail", "gmailOAuth2", "googleApi")}
	c.Ignore("authentication", "options.appendAttribution", "simple")
	defRes := "message"
	if !c.version(2) {
		defRes = "draft"
	}
	res, op := resOp(c, defRes, map[string]string{"message": "send", "draft": "create", "label": "getAll", "thread": "getAll"}[c.Literal("resource", defRes)])
	switch res + "." + op {
	case "message.send":
		cfg["operation"] = "send_message"
		if c.version(2) {
			cfg["to"] = c.Str("sendTo", "")
			cfg["body"] = c.Str("message", "")
			cfg["body_type"] = c.Literal("emailType", "html")
			gmailOptions(c, cfg, "options")
		} else {
			cfg["to"] = joinList(c, "toList")
			if c.Bool("includeHtml", false) {
				cfg["body"], cfg["body_type"] = c.Str("htmlMessage", ""), "html"
				c.Ignore("message")
			} else {
				cfg["body"], cfg["body_type"] = c.Str("message", ""), "text"
			}
			gmailOptions(c, cfg, "additionalFields")
		}
		cfg["subject"] = c.Str("subject", "")
	case "message.reply":
		cfg["operation"] = "reply"
		cfg["message_id"] = c.Str("messageId", "")
		cfg["body"] = c.Str("message", "")
		cfg["body_type"] = c.Literal("emailType", "html")
		c.Ignore("threadId", "subject")
		gmailOptions(c, cfg, "options")
	case "message.get":
		cfg["operation"] = "get_message"
		cfg["message_id"] = c.Str("messageId", "")
		c.Ignore("options", "additionalFields")
	case "message.getAll":
		cfg["operation"] = "list_messages"
		cfg["max_results"] = c.Float("limit", 50)
		if c.Bool("returnAll", false) {
			cfg["max_results"] = 500.0
			c.Note("returnAll reads up to 500 messages")
		}
		q := c.Str("filters.q", c.Str("additionalFields.q", ""))
		var extra []string
		if s := c.Literal("filters.readStatus", ""); s == "unread" || s == "read" {
			extra = append(extra, "is:"+s)
		}
		if s := c.Str("filters.sender", ""); s != "" {
			extra = append(extra, "from:"+s)
		}
		if s := c.Str("filters.receivedAfter", ""); s != "" {
			extra = append(extra, "after:"+s)
			c.Note("receivedAfter is passed to Gmail's after: search; use a yyyy/mm/dd date")
		}
		if s := c.Str("filters.receivedBefore", ""); s != "" {
			extra = append(extra, "before:"+s)
		}
		cfg["query"] = strings.TrimSpace(strings.Join(append([]string{q}, extra...), " "))
		if l, ok := c.Raw("filters.labelIds").([]interface{}); ok {
			cfg["label_ids"] = c.Translate("filters.labelIds", l)
		}
		c.Ignore("filters.includeSpamTrash", "options.downloadAttachments", "options.dataPropertyAttachmentsPrefixName")
	case "message.delete":
		cfg["operation"] = "trash_message"
		cfg["message_id"] = c.Str("messageId", "")
		c.Note("the message is moved to the trash, not deleted permanently")
	case "draft.create":
		cfg["operation"] = "create_draft"
		cfg["subject"] = c.Str("subject", "")
		cfg["body"] = c.Str("message", "")
		cfg["body_type"] = c.Literal("emailType", "text")
		cfg["to"] = c.Str("options.sendTo", "")
		gmailOptions(c, cfg, "options")
	case "label.getAll":
		cfg["operation"] = "list_labels"
		c.Ignore("returnAll", "limit")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.gmail", Config: cfg}
}

func gmailOptions(c *MapContext, cfg map[string]interface{}, coll string) {
	if c.Has(coll + ".ccList") {
		cfg["cc"] = c.Str(coll+".ccList", "")
	}
	if c.Has(coll + ".bccList") {
		cfg["bcc"] = c.Str(coll+".bccList", "")
	}
	if c.Has(coll + ".replyToSenderOnly") {
		cfg["reply_all"] = !c.Bool(coll+".replyToSenderOnly", false)
	}
}

// joinList joins a list parameter into comma-separated text.
func joinList(c *MapContext, param string) string {
	switch v := c.Raw(param).(type) {
	case []interface{}:
		parts := make([]string, 0, len(v))
		for i, x := range v {
			parts = append(parts, fmt.Sprint(c.Translate(fmt.Sprintf("%s[%d]", param, i), x)))
		}
		return strings.Join(parts, ",")
	case string:
		return c.Str(param, "")
	}
	return ""
}

func mapOutlook(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("outlook")}
	c.Ignore("authentication", "output", "fields")
	res, op := resOp(c, "message", "send", map[string]string{"draft": "create", "folder": "create", "folderMessage": "getAll", "messageAttachment": "add", "calendar": "getAll", "contact": "getAll", "event": "getAll"})
	switch res + "." + op {
	case "message.send", "draft.create":
		cfg["operation"] = map[string]string{"send": "send_message", "create": "create_draft"}[op]
		cfg["to"] = c.Str("toRecipients", c.Str("additionalFields.toRecipients", ""))
		cfg["subject"] = c.Str("subject", "")
		cfg["body"] = c.Str("bodyContent", "")
		cfg["body_type"] = strings.ToLower(c.Literal("additionalFields.bodyContentType", "html"))
		if c.Has("additionalFields.ccRecipients") {
			cfg["cc"] = c.Str("additionalFields.ccRecipients", "")
		}
		if c.Has("additionalFields.bccRecipients") {
			cfg["bcc"] = c.Str("additionalFields.bccRecipients", "")
		}
	case "message.reply":
		cfg["operation"] = "reply"
		cfg["message_id"] = c.Str("messageId", "")
		if c.version(2) {
			cfg["body"] = c.Str("message", "")
			cfg["reply_all"] = !c.Bool("replyToSenderOnly", false)
		} else {
			cfg["body"] = c.Str("comment", "")
			cfg["reply_all"] = c.Literal("replyType", "reply") == "replyAll"
			if !c.Bool("send", true) {
				cfg["operation"] = "create_reply"
			}
		}
		if c.Bool("options.saveAsDraft", false) {
			cfg["operation"] = "create_reply"
		}
	case "message.get":
		cfg["operation"] = "get_message"
		cfg["message_id"] = c.Str("messageId", "")
		c.Ignore("options", "additionalFields")
	case "message.getAll", "folderMessage.getAll":
		cfg["operation"] = "list_messages"
		cfg["max_results"] = c.Float("limit", 100)
		if res == "folderMessage" {
			cfg["mailbox"] = c.Str("folderId", "inbox")
		}
		if s := c.Str("filtersUI.values.search", ""); s != "" {
			cfg["search"] = s
		}
		if s := c.Literal("filtersUI.values.filters.readStatus", ""); s == "unread" {
			cfg["unread_only"] = true
		}
		if s := c.Str("filtersUI.values.filters.sender", ""); s != "" {
			cfg["from_address"] = s
		}
		c.Ignore("returnAll", "options", "additionalFields")
	case "message.delete":
		cfg["operation"] = "delete_message"
		cfg["message_id"] = c.Str("messageId", "")
	case "draft.send":
		cfg["operation"] = "send_draft"
		cfg["message_id"] = c.Str("draftId", "")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.outlook_mail", Config: cfg}
}

func mapGoogleSheets(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("google_sheets", "googleSheetsOAuth2Api", "googleApi")}
	c.Ignore("authentication")
	res, op := resOp(c, "sheet", "read", map[string]string{"spreadsheet": "create"})
	if res != "sheet" {
		return c.OperationUnsupported(res, op)
	}
	if c.version(3) {
		cfg["spreadsheet_id"] = c.Str("documentId", "")
		cfg["sheet_name"] = c.Str("sheetName", "Sheet1")
	} else {
		cfg["spreadsheet_id"] = c.Str("sheetId", "")
		if r := c.Literal("range", "A:F"); strings.Contains(r, "!") {
			sheet, rng, _ := strings.Cut(r, "!")
			cfg["sheet_name"], cfg["range"] = sheet, rng
		} else {
			cfg["range"] = r
		}
	}
	switch op {
	case "read", "lookup":
		cfg["operation"] = "read_rows"
		cfg["use_header_row"] = true
		if len(c.paramList("filtersUI.values")) > 0 || c.Has("lookupColumn") {
			c.Warn("filtersUI", c.Raw("filtersUI"), "row filters are not supported; every row is read")
			c.Ignore("combineFilters", "lookupValue")
		}
		c.Ignore("options.dataLocationOnSheet", "options.outputFormatting", "options.returnFirstMatch", "options.returnAllMatches")
	case "append":
		return sheetsAppend(c, cfg)
	case "clear":
		cfg["operation"] = "clear_range"
		if w := c.Literal("clear", "wholeSheet"); w == "specificRange" {
			cfg["range"] = c.Str("range", "A:F")
		} else if w != "wholeSheet" {
			c.Warn("clear", w, "only whole sheets or ranges can be cleared")
		}
		c.Ignore("keepFirstRow", "startIndex", "rowsToDelete", "columnsToDelete")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.google_sheets", Config: cfg}
}

// sheetsAppend appends one row per item. Columns defined in n8n are first
// shaped by a core.set step, since the sheet node builds rows from items.
func sheetsAppend(c *MapContext, cfg map[string]interface{}) Mapping {
	cfg["operation"] = "append_rows"
	cfg["value_input_option"] = c.Literal("options.cellFormat", "USER_ENTERED")
	c.Ignore("options.locationDefine", "options.handlingExtraData", "options.useAppend")
	mode := c.Literal("columns.mappingMode", c.Literal("dataMode", "defineBelow"))
	cols := map[string]interface{}{}
	if v, ok := c.Raw("columns.value").(map[string]interface{}); ok {
		cols = v
	}
	for _, f := range c.paramList("fieldsUi.fieldValues") {
		cols[fmt.Sprint(f["fieldId"])] = f["fieldValue"]
	}
	c.Ignore("columns", "fieldsUi")
	m := Mapping{Type: "service.google_sheets", Config: cfg}
	if mode == "autoMapInputData" || len(cols) == 0 {
		c.Note("each item's fields become a row in key order; n8n matched them to the sheet's header row")
		return m
	}
	var assigns []interface{}
	for _, k := range sortedKeys(cols) {
		assigns = append(assigns, map[string]interface{}{"field": k, "value": c.Translate("columns."+k, setValueString(cols[k])), "type": "string"})
	}
	c.Note("columns are written in alphabetical order; n8n matched them to the sheet's header row")
	return Mapping{Type: "core.set", EntrySuffix: " (columns)", Config: map[string]interface{}{"assignments": assigns, "include_input": false},
		Chain: []ChainStep{{Type: "service.google_sheets", Config: cfg}}}
}

func mapGoogleDrive(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("google_drive", "googleDriveOAuth2Api", "googleApi")}
	c.Ignore("authentication", "driveId", "options.simplifyOutput")
	res, op := resOp(c, "file", "upload", map[string]string{"folder": "create", "fileFolder": "search", "drive": "create"})
	switch res + "." + op {
	case "file.upload":
		cfg["operation"] = "upload_file"
		cfg["file_name"] = c.Str("name", "")
		cfg["parent_folder_id"] = c.Str("folderId", "")
		cfg["file_path"] = ""
		c.Ignore("inputDataFieldName", "binaryPropertyName", "binaryData")
		c.Warn("inputDataFieldName", nil, "upload_file sends a file from disk: set file_path (n8n uploaded binary data)")
	case "file.download":
		cfg["operation"] = "download_file"
		cfg["file_id"] = c.Str("fileId", "")
		cfg["file_path"] = c.Str("options.fileName", "")
		c.Ignore("options.binaryPropertyName", "options.googleFileConversion")
		c.Note("the file is saved to file_path on disk (n8n returned binary data)")
	case "file.deleteFile", "file.delete":
		cfg["operation"] = "delete_file"
		cfg["file_id"] = c.Str("fileId", "")
		c.Ignore("options.deletePermanently")
	case "file.share":
		cfg["operation"] = "share_file"
		cfg["file_id"] = c.Str("fileId", "")
		c.Warn("permissionsUi", c.Raw("permissionsUi"), "share_file shares with mono-agent's default permission; set roles in Drive")
	case "folder.create":
		cfg["operation"] = "create_folder"
		cfg["file_name"] = c.Str("name", "")
		cfg["parent_folder_id"] = c.Str("folderId", c.Str("options.parents", ""))
	case "fileFolder.search", "file.list":
		cfg["operation"] = "list_files"
		q := c.Str("queryString", "")
		if c.Literal("searchMethod", "name") == "name" && q != "" && res == "fileFolder" {
			q = "name contains '" + q + "'"
		}
		cfg["query"] = q
		cfg["parent_folder_id"] = c.Str("filter.folderId", "")
		c.Ignore("returnAll", "limit", "useQueryString", "filter", "options", "queryFilters")
	case "file.get":
		cfg["operation"] = "get_file"
		cfg["file_id"] = c.Str("fileId", "")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.google_drive", Config: cfg}
}
