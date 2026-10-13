package n8nimport

import (
	"encoding/json"
	"strings"
)

func init() {
	Register("n8n-nodes-base.slack", mapSlack)
	Register("n8n-nodes-base.telegram", mapTelegram)
	Register("n8n-nodes-base.discord", mapDiscord)
	Register("n8n-nodes-base.twilio", mapTwilio)
	Register("n8n-nodes-base.whatsApp", mapWhatsApp)
}

// resOp reads resource and operation. n8n omits parameters equal to their
// default, so a missing operation takes the default of the resource
// (defOps[resource]) or defOp.
func resOp(c *MapContext, defRes, defOp string, defOps ...map[string]string) (string, string) {
	res := c.Literal("resource", defRes)
	if len(defOps) > 0 {
		if op, ok := defOps[0][res]; ok {
			defOp = op
		}
	}
	return res, c.Literal("operation", defOp)
}

func mapSlack(c *MapContext) Mapping {
	cred := c.Credential("slack", "slackApi", "slackOAuth2Api")
	c.Ignore("authentication", "otherOptions.includeLinkToWorkflow")
	res, op := resOp(c, "message", "post", map[string]string{"channel": "create", "file": "upload", "user": "info", "reaction": "add", "star": "add", "userGroup": "create"})
	cfg := map[string]interface{}{"credential_id": cred}
	switch res + "." + op {
	case "message.post", "message.postEphemeral":
		cfg["operation"] = "post_message"
		if c.version(2) {
			if c.Literal("select", "channel") == "user" {
				cfg["channel"] = c.Str("user", "")
				c.Note("a message to a user is posted to the user's direct-message channel")
				c.Ignore("channelId")
			} else {
				cfg["channel"] = c.Str("channelId", "")
				c.Ignore("user")
			}
		} else {
			cfg["channel"] = c.Str("channel", "")
			if op == "postEphemeral" {
				c.Warn("operation", op, "is sent as a normal message")
				c.Ignore("user")
			}
		}
		cfg["text"] = c.Str("text", "")
		switch mt := c.Literal("messageType", "text"); mt {
		case "block":
			if b := slackBlocks(c, "blocksUi"); b != nil {
				cfg["blocks"] = b
			}
		case "attachment":
			c.Unsupported("attachments")
		}
		if c.Bool("jsonParameters", false) {
			if b := slackBlocks(c, "blocksJson"); b != nil {
				cfg["blocks"] = b
			}
			if c.Has("attachmentsJson") {
				c.Unsupported("attachmentsJson")
			}
		}
	case "file.upload":
		cfg["operation"] = "upload_file"
		ch := c.Str("options.channelId", "")
		if ch == "" {
			if ids, ok := c.Raw("options.channelIds").([]interface{}); ok && len(ids) > 0 {
				ch = c.Translate("options.channelIds", ids[0]).(string)
			}
		}
		cfg["channel"] = ch
		cfg["filename"] = c.Str("options.fileName", "")
		cfg["file_path"] = ""
		c.Ignore("binaryPropertyName", "binaryData")
		c.Warn("binaryPropertyName", nil, "upload_file sends a file from disk: set file_path (n8n uploaded binary data)")
		if c.Has("fileContent") {
			c.Unsupported("fileContent")
		}
	case "user.info":
		cfg["operation"] = "get_user"
		cfg["user_id"] = c.Str("user", "")
	case "channel.getAll":
		cfg["operation"] = "list_channels"
		c.Ignore("returnAll", "limit")
		if c.Has("filters") {
			c.Unsupported("filters")
		}
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "comm.slack", Config: cfg}
}

// slackBlocks reads Block Kit JSON ({"blocks": […]} or […]).
func slackBlocks(c *MapContext, param string) interface{} {
	raw := c.Raw(param)
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	if !strings.HasPrefix(s, "=") {
		var v interface{}
		if json.Unmarshal([]byte(s), &v) == nil {
			if m, ok := v.(map[string]interface{}); ok && m["blocks"] != nil {
				return escapeDeep(m["blocks"])
			}
			return escapeDeep(v)
		}
	}
	c.Warn(param, s, "Block Kit JSON set by an expression is not supported; the plain text is sent")
	return nil
}

func mapTelegram(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("telegram", "telegramApi")}
	res, op := resOp(c, "message", "sendMessage", map[string]string{"chat": "get", "callback": "answerQuery", "file": "get"})
	if res != "message" {
		return c.OperationUnsupported(res, op)
	}
	cfg["chat_id"] = c.Str("chatId", "")
	switch op {
	case "sendMessage":
		cfg["operation"] = "send_message"
		cfg["message"] = c.Str("text", "")
	case "sendPhoto":
		cfg["operation"] = "send_photo"
		if c.Bool("binaryData", false) {
			c.Warn("binaryData", true, "send_photo takes a URL or file path in photo_url")
			c.Ignore("binaryPropertyName")
		}
		cfg["photo_url"] = c.Str("file", "")
		cfg["message"] = c.Str("additionalFields.caption", "")
	default:
		return c.OperationUnsupported(res, op)
	}
	switch pm := c.Literal("additionalFields.parse_mode", "Markdown"); pm {
	case "MarkdownV2":
		cfg["parse_mode"] = "Markdown"
		c.Warn("additionalFields.parse_mode", pm, "sent as Markdown")
	default:
		cfg["parse_mode"] = pm
	}
	if c.Literal("replyMarkup", "none") != "none" {
		c.Unsupported("replyMarkup")
		c.Ignore("inlineKeyboard", "replyKeyboard", "replyKeyboardOptions", "replyKeyboardRemove", "forceReply")
	}
	c.Ignore("additionalFields.appendAttribution", "additionalFields.disable_web_page_preview")
	return Mapping{Type: "comm.telegram", Config: cfg}
}

// discordWebhook posts to a Discord webhook URL with http.request.
func discordWebhook(c *MapContext, url interface{}, textParam string) Mapping {
	body := map[string]interface{}{"content": c.Str(textParam, "")}
	if u := c.Str("options.username", ""); u != "" {
		body["username"] = u
	}
	if a := c.Str("options.avatarUrl", c.Str("options.avatar_url", "")); a != "" {
		body["avatar_url"] = a
	}
	c.Ignore("options.tts", "options.wait", "options.flags")
	return Mapping{Type: "http.request", Config: map[string]interface{}{"method": "POST", "url": url, "body_type": "json", "body": body}}
}

func mapDiscord(c *MapContext) Mapping {
	if !c.version(2) {
		return discordWebhook(c, c.Str("webhookUri", ""), "text")
	}
	auth := c.Literal("authentication", "botToken")
	if auth == "webhook" {
		c.Ignore("operation")
		return discordWebhook(c, c.Secret("webhook_url", "discordWebhookApi"), "content")
	}
	res, op := resOp(c, "channel", "create", map[string]string{"message": "send", "member": "getAll"})
	cfg := map[string]interface{}{"credential_id": c.Credential("discord", "discordBotApi", "discordOAuth2Api")}
	c.Ignore("guildId")
	switch res + "." + op {
	case "message.send":
		if c.Literal("sendTo", "channel") == "user" {
			c.Ignore("userId")
			return c.OperationUnsupported(res, "send to user")
		}
		cfg["operation"] = "send_message"
		cfg["channel_id"] = c.Str("channelId", "")
		cfg["text"] = c.Str("content", "")
		if e, ok := c.Raw("embeds").(map[string]interface{}); ok && len(e) > 0 {
			c.Unsupported("embeds")
		}
		c.Ignore("embeds", "files")
	case "message.getAll":
		cfg["operation"] = "list_messages"
		cfg["channel_id"] = c.Str("channelId", "")
		cfg["limit"] = c.Float("limit", 50)
		c.Ignore("returnAll", "simplify")
	case "message.react":
		cfg["operation"] = "add_reaction"
		cfg["channel_id"] = c.Str("channelId", "")
		cfg["message_id"] = c.Str("messageId", "")
		cfg["emoji"] = c.Str("emoji", "")
	case "channel.getAll":
		cfg["operation"] = "list_channels"
		cfg["guild_id"] = c.Str("guildId", "")
		c.Ignore("returnAll", "limit", "filter")
	default:
		return c.OperationUnsupported(res, op)
	}
	c.Ignore("authentication")
	return Mapping{Type: "service.discord", Config: cfg}
}

func mapTwilio(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("twilio", "twilioApi"),
		"from": c.Str("from", ""), "to": c.Str("to", "")}
	res, op := resOp(c, "sms", "send", map[string]string{"call": "make"})
	switch res + "." + op {
	case "sms.send":
		cfg["operation"] = "send_sms"
		if c.Bool("toWhatsapp", false) {
			cfg["operation"] = "send_whatsapp"
		}
		cfg["body"] = c.Str("message", "")
	case "call.make":
		cfg["operation"] = "make_call"
		if c.Bool("twiml", false) {
			cfg["twiml"] = c.Str("message", "")
		} else {
			cfg["twiml"] = "<Response><Say>" + c.Str("message", "") + "</Say></Response>"
		}
		if c.Has("options.statusCallback") {
			c.Unsupported("options.statusCallback")
		}
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "comm.twilio", Config: cfg}
}

func mapWhatsApp(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("whatsapp", "whatsAppApi"),
		"phone_number_id": c.Str("phoneNumberId", ""), "to": c.Str("recipientPhoneNumber", "")}
	res, op := resOp(c, "message", "sendTemplate", map[string]string{"media": "mediaUpload"})
	if res != "message" {
		return c.OperationUnsupported(res, op)
	}
	switch op {
	case "send":
		switch mt := c.Literal("messageType", "text"); mt {
		case "text":
			cfg["operation"] = "send_message"
			cfg["text"] = c.Str("textBody", "")
			c.Ignore("additionalFields.previewUrl")
		case "image", "video", "audio", "document":
			cfg["operation"] = "send_media"
			if c.Literal("mediaPath", "useMediaLink") != "useMediaLink" {
				c.Warn("mediaPath", c.Raw("mediaPath"), "send_media needs a public media URL")
				c.Ignore("mediaId", "mediaPropertyName")
			}
			cfg["media_url"] = c.Str("mediaLink", "")
			c.Ignore("additionalFields.mediaCaption", "additionalFields.mediaFilename", "mediaFilename")
		default:
			return c.OperationUnsupported(res, "send "+mt)
		}
	case "sendTemplate":
		cfg["operation"] = "send_template"
		name, lang, _ := strings.Cut(c.Literal("template", ""), "|")
		cfg["template_name"] = name
		if lang != "" {
			cfg["template_language"] = lang
		}
		if c.Has("components") {
			c.Unsupported("components")
		}
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "comm.whatsapp", Config: cfg}
}
