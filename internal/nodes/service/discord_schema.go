package service

// DiscordNodeSchema documents the config keys DiscordNode.Execute reads out
// of its map[string]interface{} config — see
// internal/nodes/control/set_schema.go's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar. It is never constructed or
// used at runtime; this struct exists solely to generate
// internal/workflow/schemas/service.discord.json.
//
// credential_platform: discord
type DiscordNodeSchema struct {
	Auth string `json:"auth" schema:"label=Authentication,type=select,options=bot|webhook,default=bot,help=bot uses the Discord connection's bot token (all operations); webhook posts through a channel webhook URL (send_message and send_embed only)."`

	CredentialID string `json:"credential_id" schema:"label=Discord Connection,type=credential_picker,help=Bot connection. Not needed with webhook authentication.,depends_on_key=auth,depends_on_values=bot"`

	WebhookURL string `json:"webhook_url" schema:"label=Webhook URL,type=text,placeholder=https://discord.com/api/webhooks/...,help=Channel settings → Integrations → Webhooks → Copy Webhook URL.,depends_on_key=auth,depends_on_values=webhook"`

	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=send_message|send_embed|list_messages|add_reaction|list_channels,default=send_message"`

	ChannelID string `json:"channel_id" schema:"label=Channel ID,type=text,help=Not used with webhook authentication (the webhook decides the channel).,depends_on_key=operation,depends_on_values=send_message|send_embed|list_messages|add_reaction"`

	Text string `json:"text" schema:"label=Message,type=textarea,rows=3,help=Required for send_message; optional text above the embed for send_embed.,depends_on_key=operation,depends_on_values=send_message|send_embed"`

	EmbedTitle string `json:"embed_title" schema:"label=Embed Title,type=text,depends_on_key=operation,depends_on_values=send_embed"`

	EmbedDescription string `json:"embed_description" schema:"label=Embed Description,type=textarea,rows=3,depends_on_key=operation,depends_on_values=send_embed"`

	EmbedColor float64 `json:"embed_color" schema:"label=Embed Color,type=number,help=Decimal color， e.g. 5814783 for #5865F2.,depends_on_key=operation,depends_on_values=send_embed"`

	Username string `json:"username" schema:"label=Display Name Override,type=text,help=Webhook only: post under this name.,depends_on_key=auth,depends_on_values=webhook"`

	AvatarURL string `json:"avatar_url" schema:"label=Avatar URL Override,type=text,help=Webhook only: post with this avatar.,depends_on_key=auth,depends_on_values=webhook"`

	Limit float64 `json:"limit" schema:"label=Limit,type=number,default=50,depends_on_key=operation,depends_on_values=list_messages"`

	MessageID string `json:"message_id" schema:"label=Message ID,type=text,depends_on_key=operation,depends_on_values=add_reaction"`

	Emoji string `json:"emoji" schema:"label=Emoji,type=text,placeholder=e.g. 👍 or unicode name,depends_on_key=operation,depends_on_values=add_reaction"`

	GuildID string `json:"guild_id" schema:"label=Server (Guild) ID,type=text,depends_on_key=operation,depends_on_values=list_channels"`
}
