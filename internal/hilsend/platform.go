package hilsend

import "strings"

// PlatformOf is the platform a node type sends on, the platform an
// always-allow rule and a card's account name: the node's own name, never
// its family prefix, so a rule for a Discord channel never matches a
// Reddit id that reads the same (second security review of #871).
// service.discord, comm.discord and the alias discord are all "discord";
// the mail nodes are "email", "outlook" and "gmail"; a browser node is its
// platform (instagram.send_dms is "instagram").
func PlatformOf(nodeType string) string {
	family, name, dotted := strings.Cut(nodeType, ".")
	if !dotted {
		name = family
	} else if family != "comm" && family != "service" {
		return family
	}
	switch name {
	case "email_send":
		return "email"
	case "outlook_send", "outlook_mail":
		return "outlook"
	}
	return name
}
