package apipkg

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The send lint (TestCatalogSendsAreDecided): an op has to say whether it
// sends ("sends": {…}, or "sends": false with a sends_reason) when
//   - its package is a messaging one (it has an annotated send, or it is a
//     mail, SMS, chat or ticketing service: messagingPackages) and the op
//     writes: any method but GET, HEAD, OPTIONS or PROPFIND, a templated
//     method included, whatever the op is called; or
//   - a send word is in its name, path or description, it is not a read (a
//     read verb first and no send verb in its name), and it writes or a send
//     verb names it (a GET to /sendhttp.php).
//
// An op on settings ("updateNotificationSettings") counts its send verbs
// only, not its send nouns.
var (
	sendWords = map[string]bool{"send": true, "message": true, "messages": true, "sms": true, "mms": true, "email": true,
		"emails": true, "mail": true, "notification": true, "notifications": true, "notify": true, "publish": true,
		"reply": true, "replies": true, "comment": true, "comments": true, "post": true, "posts": true, "tweet": true,
		"tweets": true, "push": true, "invite": true, "invites": true, "invitation": true, "invitations": true,
		"reinvite": true, "call": true, "calls": true, "voice": true, "campaign": true, "campaigns": true,
		"broadcast": true, "dm": true, "share": true}
	sendVerbs = map[string]bool{"send": true, "notify": true, "publish": true, "invite": true, "reinvite": true, "broadcast": true}
	readVerbs = map[string]bool{"get": true, "getall": true, "list": true, "search": true, "count": true, "check": true,
		"analyze": true, "predict": true, "mark": true, "move": true, "delete": true, "remove": true, "pause": true,
		"resolve": true, "close": true, "leave": true, "pin": true, "unpin": true, "undo": true, "soft": true, "lock": true,
		"duplicate": true, "replicate": true, "react": true, "like": true, "retweet": true, "rate": true, "execute": true,
		"status": true, "track": true}
	settingsWords = map[string]bool{"settings": true, "setting": true, "preferences": true, "preference": true,
		"prefs": true, "config": true, "configuration": true}
	readMethods = map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true, "PROPFIND": true}
	// messagingPackages are mail, SMS, chat and ticketing services with no
	// annotated send of their own: every write of theirs is decided too.
	messagingPackages = map[string]bool{"active_campaign": true, "customer_io": true, "drift": true, "egoi": true,
		"freshdesk": true, "freshservice": true, "get_response": true, "gotify": true, "halo_psa": true,
		"intercom": true, "iterable": true, "mailer_lite": true, "msg91": true, "postmark": true, "pushcut": true,
		"service_now": true, "syncro_msp": true, "the_hive": true, "vero": true, "zammad": true, "zendesk": true}
	camelHump  = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	nonWordRun = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

func opWords(s string) []string {
	var out []string
	for _, w := range nonWordRun.Split(camelHump.ReplaceAllString(s, "$1 $2"), -1) {
		if w != "" {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

// isSendVerb: a send verb, or a word one starts ("sendhttp", "notifyAll";
// a sender is not one).
func isSendVerb(w string) bool {
	return sendVerbs[w] || strings.HasPrefix(w, "notify") || (strings.HasPrefix(w, "send") && !strings.HasPrefix(w, "sender"))
}

func anyWord(words []string, is func(string) bool) bool {
	for _, w := range words {
		if is(w) {
			return true
		}
	}
	return false
}

// undecidedSends lists the ops of pkgs the lint wants a decision on.
func undecidedSends(pkgs []*Package) []string {
	var out []string
	for _, p := range pkgs {
		messaging := messagingPackages[p.ID]
		for _, o := range p.Operations {
			messaging = messaging || (o.Sends != nil && !o.Sends.None)
		}
		for _, o := range p.Operations {
			if o.Sends != nil {
				continue
			}
			writes := hasTemplate(o.Method) || !readMethods[strings.ToUpper(strings.TrimSpace(o.Method))]
			op := opWords(o.Operation)
			name := append(opWords(o.Resource), op...)
			path := opWords(o.Path)
			isSend := func(w string) bool { return sendWords[w] || isSendVerb(w) }
			if anyWord(name, func(w string) bool { return settingsWords[w] }) {
				isSend = isSendVerb
			}
			named := anyWord(name, isSend) || anyWord(path, isSend) || anyWord(opWords(o.Description), isSend)
			verbNamed := anyWord(name, isSendVerb) || anyWord(path, isSendVerb)
			read := len(op) > 0 && readVerbs[op[0]] && !anyWord(name, isSendVerb)
			if (messaging && writes) || (named && !read && (writes || verbNamed)) {
				out = append(out, p.ID+" "+o.Resource+"/"+o.Operation)
			}
		}
	}
	return out
}

// The lint can't be talked past: in a messaging package every write needs
// a decision, however it is named or sent, and anywhere a send verb in a
// name or path does, even on a GET.
func TestTheSendLintSeesPastNames(t *testing.T) {
	sends := &Sends{Action: "post"}
	chat := &Package{ID: "acme_chat", Operations: []Operation{
		{Resource: "message", Operation: "post", Method: "POST", Path: "/messages", Sends: sends},
		{Resource: "message", Operation: "deliver", Method: "{{ p.v | eq:'2' | then:'POST' | default:'GET' }}", Path: "/m"},
		{Resource: "sms", Operation: "go", Method: "GET", Path: "/sendhttp.php"},
		{Resource: "phone", Operation: "ringPhone", Method: "POST", Path: "/ring"},
		{Resource: "user", Operation: "dispatchToUser", Method: "POST", Path: "/dispatch"},
		{Resource: "ticket", Operation: "resolveTicketAndNotify", Method: "POST", Path: "/tickets/resolve"},
		{Resource: "event", Operation: "trackAndSend", Method: "POST", Path: "/track"},
		{Resource: "message", Operation: "remove", Method: "DELETE", Path: "/messages/1"},
		{Resource: "message", Operation: "getAll", Method: "GET", Path: "/messages"},
	}}
	other := &Package{ID: "acme_crm", Operations: []Operation{
		{Resource: "sms", Operation: "go", Method: "GET", Path: "/sendhttp.php"},
		{Resource: "ticket", Operation: "resolveTicketAndNotify", Method: "POST", Path: "/tickets/resolve"},
		{Resource: "event", Operation: "trackAndSend", Method: "POST", Path: "/track"},
		{Resource: "contact", Operation: "update", Method: "PATCH", Path: "/contacts/1", Description: "Emails the contact a summary."},
		{Resource: "phone", Operation: "ringPhone", Method: "POST", Path: "/ring"},
		{Resource: "message", Operation: "delete", Method: "DELETE", Path: "/messages/1"},
		{Resource: "message", Operation: "getAll", Method: "GET", Path: "/messages"},
	}}
	got := undecidedSends([]*Package{chat, other})
	want := []string{"acme_chat message/deliver", "acme_chat sms/go", "acme_chat phone/ringPhone", "acme_chat user/dispatchToUser",
		"acme_chat ticket/resolveTicketAndNotify", "acme_chat event/trackAndSend", "acme_chat message/remove",
		"acme_crm sms/go", "acme_crm ticket/resolveTicketAndNotify", "acme_crm event/trackAndSend", "acme_crm contact/update"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("undecided:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// Changing one's own settings is no send, notification and message
// settings included; a send verb still is one.
func TestTheSendLintLeavesSettingsAlone(t *testing.T) {
	p := &Package{ID: "acme_tasks", Operations: []Operation{
		{Resource: "user", Operation: "updateNotificationSettings", Method: "PATCH", Path: "/me/notification_settings"},
		{Resource: "user", Operation: "updateMessageSettings", Method: "PUT", Path: "/me/message-preferences"},
		{Resource: "user", Operation: "sendNotification", Method: "POST", Path: "/notifications"},
		{Resource: "user", Operation: "sendSettingsEmail", Method: "POST", Path: "/me/settings/email"},
	}}
	if got := undecidedSends([]*Package{p}); !reflect.DeepEqual(got, []string{"acme_tasks user/sendNotification", "acme_tasks user/sendSettingsEmail"}) {
		t.Errorf("undecided = %v", got)
	}
}
