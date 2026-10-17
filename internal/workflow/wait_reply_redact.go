package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// A reply's token can reach an agent through execution data as well as the
// waiting list RedactReplies trims: $execution.resumeUrl is an expression
// any node can use, and a node's items or error can hold the link it put in
// a message. ReplyRedactor hides those tokens in what an agent is shown:
// node items, errors, trigger data, log lines.

// ReplyTokenHidden stands in for a hidden reply token.
const ReplyTokenHidden = "[reply token hidden]"

// replyTokenLen is the length of the tokens signReply makes (43 base64url
// characters: an HMAC-SHA256, unpadded; see validReplyToken).
const replyTokenLen = 43

var (
	// replyLinkPattern is a reply link's token, the link URL-encoded or not.
	replyLinkPattern = regexp.MustCompile(`(?:/|%2[fF])reply(?:/|%2[fF])[A-Za-z0-9_-]{43}`)
	// base64urlRun is a run of base64url characters; one of exactly 43 may
	// be a bare token.
	base64urlRun = regexp.MustCompile(`[A-Za-z0-9_-]+`)
)

// ReplyRedactor hides reply tokens in text shown to an agent. A token stays
// only when every reply recorded under it is one node's and marked as
// sending nothing, as its links then are in the waiting list too. A token of
// a reply that is marked, or was opened before the mark was recorded, or is
// shared (an execution's resume URL, a chat session's: replyShared), or of
// no recorded reply at all (an execution's resume URL before its core.wait
// opens) is hidden. In a link (/reply/<token>) the token is always one; a
// bare 43-character string is one when a reply is recorded under it or it is
// the resume token of an execution being shown. With no database to tell,
// every link's token and every bare 43-character base64url string is hidden.
//
// A nil *ReplyRedactor hides nothing: the view of the user's own terminal.
type ReplyRedactor struct {
	ctx context.Context
	db  *sql.DB
	// watch are the tokens of the executions shown, hidden wherever they
	// appear, even inside a longer run of characters.
	watch []string
	mu    sync.Mutex // guards seen: a redactor may serve several goroutines
	seen  map[string]replyTokenState
}

type replyTokenState struct {
	recorded bool // a reply is recorded under the token
	hidden   bool // one of them may send, or the token is shared
}

// shown reports whether a token known to be a reply token may stay.
func (s replyTokenState) shown() bool { return s.recorded && !s.hidden }

// NewReplyRedactor hides the tokens of the replies in db (nil: every
// candidate, see ReplyRedactor). executionIDs are the executions being
// shown: their resume tokens and reply tokens are looked for anywhere.
func NewReplyRedactor(ctx context.Context, db *sql.DB, executionIDs ...string) *ReplyRedactor {
	r := &ReplyRedactor{ctx: ctx, db: db, seen: map[string]replyTokenState{}}
	for _, id := range executionIDs {
		if id == "" {
			continue
		}
		if tok, err := signReply("execution", id); err == nil {
			r.watch = append(r.watch, tok)
		}
		if db == nil {
			continue
		}
		rows, err := db.QueryContext(ctx, `SELECT DISTINCT token FROM wait_replies WHERE execution_id = ?`, id)
		if err != nil {
			continue
		}
		for rows.Next() {
			var tok string
			if rows.Scan(&tok) == nil {
				r.watch = append(r.watch, tok)
			}
		}
		rows.Close()
	}
	return r
}

// state looks token up once; a failed lookup counts as a marked reply.
func (r *ReplyRedactor) state(token string) replyTokenState {
	if r.db == nil {
		return replyTokenState{recorded: true, hidden: true}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.seen[token]; ok {
		return s
	}
	s := r.load(token)
	r.seen[token] = s
	return s
}

func (r *ReplyRedactor) load(token string) replyTokenState {
	failed := replyTokenState{recorded: true, hidden: true}
	rows, err := r.db.QueryContext(r.ctx, `SELECT spec FROM wait_replies WHERE token = ?`, token)
	if err != nil {
		return failed
	}
	defer rows.Close()
	var s replyTokenState
	for rows.Next() {
		var specJSON string
		if rows.Scan(&specJSON) != nil {
			return failed
		}
		var spec WaitReplySpec
		_ = json.Unmarshal([]byte(specJSON), &spec)
		s.recorded = true
		s.hidden = s.hidden || replyOutbound(spec) || replyShared(spec)
	}
	if rows.Err() != nil {
		return failed
	}
	return s
}

// String is s with its hidden reply tokens replaced by ReplyTokenHidden.
func (r *ReplyRedactor) String(s string) string {
	if r == nil || len(s) < replyTokenLen {
		return s
	}
	s = replyLinkPattern.ReplaceAllStringFunc(s, func(m string) string {
		cut := len(m) - replyTokenLen
		if r.state(m[cut:]).shown() {
			return m
		}
		return m[:cut] + ReplyTokenHidden
	})
	for _, tok := range r.watch {
		if strings.Contains(s, tok) && !r.state(tok).shown() {
			s = strings.ReplaceAll(s, tok, ReplyTokenHidden)
		}
	}
	return base64urlRun.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) != replyTokenLen {
			return m
		}
		if st := r.state(m); st.recorded && st.hidden {
			return ReplyTokenHidden
		}
		return m
	})
}

// Value is v with String applied to every string in it, map keys included.
func (r *ReplyRedactor) Value(v interface{}) interface{} {
	if r == nil {
		return v
	}
	switch t := v.(type) {
	case nil, bool, float64, json.Number:
		return v
	case string:
		return r.String(t)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, x := range t {
			out[r.String(k)] = r.Value(x)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = r.Value(x)
		}
		return out
	}
	// Another Go value (a node's typed output): through its JSON form.
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	s := r.String(string(b))
	if s == string(b) {
		return v
	}
	var out interface{}
	if json.Unmarshal([]byte(s), &out) != nil {
		return ReplyTokenHidden
	}
	return out
}

// Map is Value for a map (nil stays nil).
func (r *ReplyRedactor) Map(m map[string]interface{}) map[string]interface{} {
	if r == nil || m == nil {
		return m
	}
	out, _ := r.Value(m).(map[string]interface{})
	return out
}

// Items is items with their reply tokens hidden, in the JSON and in the
// binary data.
func (r *ReplyRedactor) Items(items []Item) []Item {
	if r == nil || items == nil {
		return items
	}
	out := make([]Item, len(items))
	for i, it := range items {
		out[i] = Item{JSON: r.Map(it.JSON)}
		if it.Binary != nil {
			out[i].Binary = make(map[string][]byte, len(it.Binary))
			for k, b := range it.Binary {
				out[i].Binary[r.String(k)] = []byte(r.String(string(b)))
			}
		}
	}
	return out
}

// CheckReplyEdit refuses an approval's edited data (JSON) that holds
// ReplyTokenHidden: written back, the placeholder would replace the reply
// link the run needs. A field left out of the edit keeps its value.
func CheckReplyEdit(edited string) error {
	if !strings.Contains(edited, ReplyTokenHidden) {
		return nil
	}
	return fmt.Errorf("the data holds %q, which stands in for a reply link kept from you: leave out the fields that hold it and they keep their value", ReplyTokenHidden)
}

// Execution hides the reply tokens of e, in place: its error and trigger
// data, and every node's items and error.
func (r *ReplyRedactor) Execution(e *WorkflowExecution) {
	if r == nil || e == nil {
		return
	}
	e.ErrorMessage = r.String(e.ErrorMessage)
	e.TriggerData = r.Map(e.TriggerData)
	for i := range e.Nodes {
		n := &e.Nodes[i]
		n.InputItems, n.OutputItems = r.Items(n.InputItems), r.Items(n.OutputItems)
		n.ErrorMessage, n.ErrorHint = r.String(n.ErrorMessage), r.String(n.ErrorHint)
	}
}
