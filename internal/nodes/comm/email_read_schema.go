package comm

// EmailReadNodeSchema documents the config keys EmailReadNode.Execute reads
// out of its map[string]interface{} config, via `schema:"..."` tags
// consumed by internal/tools/schemagen (see that package's doc comment for
// the tag grammar). It is never constructed or used at runtime.
//
// "tls" has no entry in the hand-written schemas/comm.email_read.json this
// replaces, even though EmailReadNode's own doc comment has documented a
// "tls" (bool, default true) config key since before this conversion.
// Generating from the doc-comment contract (rather than hand-copying the
// old JSON) surfaces that gap.
//
// credential_platform: imap
type EmailReadNodeSchema struct {
	IMAPHost string `json:"imap_host" schema:"label=IMAP Host,type=text,required,placeholder=imap.gmail.com"`

	IMAPPort float64 `json:"imap_port" schema:"label=IMAP Port,type=number,required,default=993"`

	Username string `json:"username" schema:"label=Username,type=text,required"`

	Password string `json:"password" schema:"label=Password / App Password,type=password,required"`

	TLS bool `json:"tls" schema:"label=Use TLS,type=boolean,default=true"`

	Mailbox string `json:"mailbox" schema:"label=Mailbox,type=text,default=INBOX"`

	Limit float64 `json:"limit" schema:"label=Max Emails,type=number,default=20"`

	UnreadOnly bool `json:"unread_only" schema:"label=Unread Only,type=boolean,default=false"`

	SinceLastRun bool `json:"since_last_run" schema:"label=Only New Since Last Run,type=boolean,default=false,help=Return only messages with a UID above the highest one this node returned in an earlier run. The first run returns the newest Max Emails as usual; later runs return the oldest Max Emails new messages so a backlog is read over the next runs. Remembered per node and account and mailbox; starts over when the server changes the mailbox's UIDVALIDITY."`
}
