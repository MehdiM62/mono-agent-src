package comm

import "github.com/monoes/mono-agent/internal/workflow"

// RegisterAll registers all communication node types in the given registry.
func RegisterAll(r *workflow.NodeTypeRegistry) {
	r.Register("comm.email_send", func() workflow.NodeExecutor { return &EmailSendNode{} })
	r.Register("comm.email_read", func() workflow.NodeExecutor { return &EmailReadNode{} })
	r.Register("comm.slack", func() workflow.NodeExecutor { return &SlackNode{} })
	r.Register("comm.telegram", func() workflow.NodeExecutor { return &TelegramNode{} })
	r.Register("comm.twilio", func() workflow.NodeExecutor { return &TwilioNode{} })
	r.Register("comm.whatsapp", func() workflow.NodeExecutor { return &WhatsAppNode{} })

	// These platforms used to exist twice, once here and once as service.*.
	// The service.* node is now the single canonical one (the union of both
	// operation sets) and runs the old comm.* configs unchanged; the old
	// type ids stay resolvable for saved workflows but are not listed.
	r.Alias("comm.bluesky", "service.bluesky")
	r.Alias("comm.mastodon", "service.mastodon")
	r.Alias("comm.reddit", "service.reddit")
	r.Alias("comm.discord", "service.discord")
	r.Alias("comm.outlook_read", "service.outlook_mail")
	r.Alias("comm.outlook_send", "service.outlook_mail")
}
