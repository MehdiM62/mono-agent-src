package comm

import (
	"context"
	"strconv"

	"github.com/monoes/mono-agent/internal/workflow"
)

// imapCursor is what a since_last_run comm.email_read remembers about its
// previous run of a mailbox: the mailbox's UIDVALIDITY and the highest UID
// it returned. A UIDVALIDITY change means the server renumbered the mailbox,
// so the stored UID no longer means anything and the run starts over.
type imapCursor struct {
	UIDValidity uint32 `json:"uid_validity"`
	LastUID     uint32 `json:"last_uid"`
}

// imapSince is the since_last_run state of one comm.email_read run.
type imapSince struct {
	sd   *workflow.StaticData
	key  string
	prev imapCursor
}

// loadIMAPSince reads the cursor of input's node for this account and
// mailbox, so pointing the node elsewhere starts over.
func loadIMAPSince(ctx context.Context, input workflow.NodeInput, opts imapReadOpts) (*imapSince, error) {
	sd, err := workflow.StaticDataFor(ctx, input, workflow.StateScopeNode)
	if err != nil {
		return nil, err
	}
	s := &imapSince{sd: sd, key: "imap_cursor:" + opts.username + "@" + opts.host + ":" + strconv.Itoa(opts.port) + "/" + opts.mailbox}
	if _, err := sd.Get(ctx, s.key, &s.prev); err != nil {
		return nil, err
	}
	return s, nil
}

// save stores the cursor after a successful read: the highest UID returned,
// or the one the read started after when it returned nothing.
func (s *imapSince) save(ctx context.Context, res *imapReadResult) error {
	next := imapCursor{UIDValidity: res.uidValidity, LastUID: res.afterUID}
	if res.maxUID > next.LastUID {
		next.LastUID = res.maxUID
	}
	return s.sd.Set(ctx, s.key, next)
}
