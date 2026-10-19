package service

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/nodes/comm"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Authentication modes of service.outlook_mail ("auth" config key).
const (
	outlookAuthOAuth       = "oauth"        // Microsoft Graph with an OAuth access token
	outlookAuthAppPassword = "app_password" // IMAP (read) / SMTP (send) with email + app password
)

// outlookIMAPRead and outlookSMTPSend are vars so tests can swap the
// network transports for recorders.
var (
	outlookIMAPRead = (&comm.OutlookReadNode{}).Execute
	outlookSMTPSend = (&comm.OutlookSendNode{}).Execute
)

// outlookAuthMode picks the authentication mode. An explicit "auth" wins;
// otherwise the keys a saved connection injected decide: an OAuth
// connection brings "access_token", an app-password one brings "email" and
// "app_password" (older configs typed "password").
func outlookAuthMode(config map[string]interface{}) (string, error) {
	switch auth := strVal(config, "auth"); auth {
	case outlookAuthOAuth, outlookAuthAppPassword:
		return auth, nil
	case "":
	default:
		return "", fmt.Errorf("outlook_mail: unknown auth %q (want oauth or app_password)", auth)
	}
	if strVal(config, "access_token") == "" &&
		(strVal(config, "app_password") != "" || strVal(config, "password") != "") {
		return outlookAuthAppPassword, nil
	}
	return outlookAuthOAuth, nil
}

// outlookAppPasswordExecute runs list_messages over IMAP and send_message
// over SMTP (outlook.office365.com / smtp-mail.outlook.com), with the
// config keys and output shapes of the legacy comm.outlook_read and
// comm.outlook_send nodes.
//
// Legacy configs carry no "operation": a comm.outlook_send config always
// has "to"/"subject"/"body" keys and a comm.outlook_read one never does, so
// their presence selects send_message, their absence list_messages.
func outlookAppPasswordExecute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	operation := workflow.ConfigOperation(config)
	if operation == "" {
		operation = "list_messages"
		for _, k := range []string{"to", "subject", "body"} {
			if _, ok := config[k]; ok {
				operation = "send_message"
				break
			}
		}
	}
	switch operation {
	case "list_messages":
		if _, ok := config["limit"]; !ok {
			if _, ok := config["max_results"]; ok {
				config = withDefault(config, "limit", config["max_results"])
			}
		}
		return outlookIMAPRead(ctx, input, config)
	case "send_message":
		return outlookSMTPSend(ctx, input, config)
	default:
		return nil, fmt.Errorf("outlook_mail: operation %q needs auth=oauth (Microsoft Graph); with an app password only list_messages and send_message are available", operation)
	}
}
