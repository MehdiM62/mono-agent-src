package comm

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/workflow"
)

// EmailReadNode fetches emails from an IMAP server.
// Type: "comm.email_read"
//
// Config fields:
//
//	"imap_host"      (string, required): IMAP server hostname
//	"imap_port"      (int, default 993): IMAP server port
//	"username"       (string): IMAP auth username
//	"password"       (string): IMAP auth password
//	"tls"            (bool, default true): use TLS
//	"mailbox"        (string, default "INBOX"): mailbox to read
//	"limit"          (int, default 10): max messages to fetch
//	"unread_only"    (bool, default false): only return unseen messages
//	"since_last_run" (bool, default false): only return messages with a UID
//	                 above the highest one an earlier run returned
//	                 (email_read_cursor.go)
//	"baseline"       (bool, default false): with since_last_run, the first
//	                 run only records the mailbox's highest UID and returns
//	                 nothing
//
// Returns each message as an Item with fields, newest first:
// "uid", "subject", "from", "date", "body", "message_id", "read"
type EmailReadNode struct{}

func (n *EmailReadNode) Type() string { return "comm.email_read" }

// CredentialDestinationKeys: the server the connection's password goes to
// (workflow.CheckCredentialDestinations).
func (n *EmailReadNode) CredentialDestinationKeys() [][]string {
	return [][]string{{"imap_host"}, {"imap_port"}}
}

func (n *EmailReadNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("comm.email_read", config) {
		return apipkg.ExecuteExtension(ctx, "comm.email_read", input, config)
	}
	opts := imapReadOpts{port: 993, useTLS: true, mailbox: "INBOX", limit: 10}
	opts.host, _ = config["imap_host"].(string)
	if opts.host == "" {
		return nil, fmt.Errorf("comm.email_read: imap_host is required")
	}
	opts.username, _ = config["username"].(string)
	if opts.username == "" {
		return nil, fmt.Errorf("comm.email_read: username is required")
	}
	opts.password, _ = config["password"].(string)
	if opts.password == "" {
		return nil, fmt.Errorf("comm.email_read: password is required")
	}
	if v := intConfig(config, "imap_port"); v > 0 {
		opts.port = v
	}
	if v, ok := config["tls"].(bool); ok {
		opts.useTLS = v
	}
	if v, _ := config["mailbox"].(string); v != "" {
		opts.mailbox = v
	}
	if v := intConfig(config, "limit"); v > 0 {
		opts.limit = v
	}
	opts.unreadOnly, _ = config["unread_only"].(bool)

	var since *imapSince
	if v, _ := config["since_last_run"].(bool); v {
		var err error
		if since, err = loadIMAPSince(ctx, input, opts); err != nil {
			return nil, fmt.Errorf("comm.email_read: since_last_run: %w", err)
		}
		opts.since = &since.prev
		opts.baselineOnly = !since.found && workflow.PollBaseline(config)
	}

	res, err := readIMAPMail(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("comm.email_read: %w", err)
	}
	if since != nil {
		if err := since.save(ctx, res); err != nil {
			return nil, fmt.Errorf("comm.email_read: since_last_run: %w", err)
		}
	}

	items := make([]workflow.Item, len(res.messages))
	for i, m := range res.messages {
		items[i] = workflow.NewItem(m)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

// intConfig reads a whole number given as a JSON number or an int.
func intConfig(config map[string]interface{}, key string) int {
	switch v := config[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}
