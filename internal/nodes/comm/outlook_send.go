package comm

import (
	"context"
	"fmt"
	"net"
	"net/smtp"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

// OutlookSendNode sends an email via Outlook / Hotmail SMTP.
//
// It is no longer registered as a node type of its own: service.outlook_mail
// runs it for send_message with app-password authentication, and the
// legacy "comm.outlook_send" type is an alias of service.outlook_mail.
//
// Config fields:
//
//	"email"       (string, required): Outlook/Hotmail sender address (also the SMTP username)
//	"password"    (string, required): Account password or app password
//	"to"          (string or []string, required): recipient(s); a string may
//	              be comma/semicolon-separated for multiple addresses
//	"cc"          (string or []string): CC recipients
//	"bcc"         (string or []string): BCC recipients
//	"subject"     (string, required): email subject
//	"body"        (string, required): email body
//	"body_type"   (string): "text" (default) or "html"
//	"attachments" ([]string): file paths to attach
//	"in_reply_to" (string): original message's Message-ID header (from
//	              comm.outlook_read), to thread this as a reply instead of a
//	              new message; auto-prefixes subject with "Re: "
//	"references"  (string): thread's References header chain; defaults to
//	              in_reply_to when omitted
//
// Uses Outlook SMTP: smtp-mail.outlook.com:587 with STARTTLS.
type OutlookSendNode struct{}

func (n *OutlookSendNode) Type() string { return "comm.outlook_send" }

func (n *OutlookSendNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("comm.outlook_send", config) {
		return apipkg.ExecuteExtension(ctx, "comm.outlook_send", input, config)
	}
	const smtpHost = "smtp-mail.outlook.com"
	const smtpPort = 587

	email, _ := config["email"].(string)
	if email == "" {
		return nil, fmt.Errorf("outlook_mail send_message (app password): email is required")
	}

	// Support both "password" and "app_password" keys (connection saves as app_password).
	password, _ := config["password"].(string)
	if password == "" {
		password, _ = config["app_password"].(string)
	}
	if password == "" {
		return nil, fmt.Errorf("outlook_mail send_message (app password): password is required")
	}

	toAddrs := toStringSlice(config["to"])
	if len(toAddrs) == 0 {
		return nil, fmt.Errorf("outlook_mail send_message (app password): to is required")
	}

	ccAddrs := toStringSlice(config["cc"])
	bccAddrs := toStringSlice(config["bcc"])

	subject, _ := config["subject"].(string)
	if subject == "" {
		return nil, fmt.Errorf("outlook_mail send_message (app password): subject is required")
	}

	body, _ := config["body"].(string)
	if body == "" {
		return nil, fmt.Errorf("outlook_mail send_message (app password): body is required")
	}

	bodyType := "text"
	if bt, ok := config["body_type"].(string); ok && bt != "" {
		bodyType = bt
	}

	var attachmentPaths []string
	if ap, ok := config["attachments"]; ok {
		switch v := ap.(type) {
		case []string:
			attachmentPaths = v
		case []interface{}:
			for _, a := range v {
				if s, ok := a.(string); ok {
					attachmentPaths = append(attachmentPaths, s)
				}
			}
		}
	}
	// C-46: attachments are local files sent away; confine them to the org
	// workdir when a role's grant started this run.
	attachmentPaths, err := fsconfine.Paths(ctx, attachmentPaths)
	if err != nil {
		return nil, fmt.Errorf("outlook_mail send_message (app password): attachments: %w", err)
	}

	// Optional threading: pass the original message's Message-ID header (get
	// it from comm.outlook_read) as in_reply_to to make this a reply on the
	// same thread instead of a disconnected new message.
	inReplyTo, _ := config["in_reply_to"].(string)
	references, _ := config["references"].(string)
	if references == "" {
		references = inReplyTo
	}
	if inReplyTo != "" {
		subject = prefixReplySubject(subject)
	}

	allRecipients := make([]string, 0, len(toAddrs)+len(ccAddrs)+len(bccAddrs))
	allRecipients = append(allRecipients, toAddrs...)
	allRecipients = append(allRecipients, ccAddrs...)
	allRecipients = append(allRecipients, bccAddrs...)

	msgBytes, err := buildMIMEMessage(email, toAddrs, ccAddrs, subject, body, bodyType, attachmentPaths, inReplyTo, references)
	if err != nil {
		return nil, fmt.Errorf("outlook_mail send_message (app password): build message: %w", err)
	}

	addr := net.JoinHostPort(smtpHost, fmt.Sprintf("%d", smtpPort))
	auth := smtp.PlainAuth("", email, password, smtpHost)

	if err := smtp.SendMail(addr, auth, email, allRecipients, msgBytes); err != nil {
		return nil, fmt.Errorf("outlook_mail send_message (app password): %w", err)
	}

	result := workflow.NewItem(map[string]interface{}{
		"sent": true,
		"from": email,
		"to":   toStringInterfaceSlice(toAddrs),
	})
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{result}}}, nil
}
