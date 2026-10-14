package service

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// gmailCursor is what a since_last_run list_messages remembers about its
// previous run: the newest internalDate (epoch ms) it saw and the ids of the
// messages dated exactly then, the only ones a later run could list again
// without being older than the cursor.
type gmailCursor struct {
	NewestMs int64    `json:"newest_ms"`
	IDs      []string `json:"ids"`
}

// gmailSince is the since_last_run state of one list_messages run.
type gmailSince struct {
	sd    *workflow.StaticData
	key   string
	prev  gmailCursor
	found bool
	// baseline makes a run without a stored cursor record what it lists
	// and return nothing (workflow.PollBaseline).
	baseline bool
}

// loadGmailSince reads the cursor of input's node for this query and label
// filter, so changing either starts over.
func loadGmailSince(ctx context.Context, input workflow.NodeInput, query string, labelIDs []string, baseline bool) (*gmailSince, error) {
	sd, err := workflow.StaticDataFor(ctx, input, workflow.StateScopeNode)
	if err != nil {
		return nil, err
	}
	s := &gmailSince{sd: sd, key: "gmail_cursor:" + query + "\x00" + strings.Join(labelIDs, ","), baseline: baseline}
	if s.found, err = sd.Get(ctx, s.key, &s.prev); err != nil {
		return nil, err
	}
	return s, nil
}

// query narrows q to messages from the cursor's second on. Gmail's after:
// takes epoch seconds, so the boundary second is listed again and the id
// dedupe in filter drops what the previous run already emitted.
func (s *gmailSince) query(q string) string {
	if !s.found || s.prev.NewestMs == 0 {
		return q
	}
	after := "after:" + strconv.FormatInt(s.prev.NewestMs/1000-1, 10)
	if q == "" {
		return after
	}
	return q + " " + after
}

// filter fetches each listed message's internalDate, adds it to the item
// and keeps the messages the previous run did not see: an id not already
// emitted and not dated before the newest message seen so far. The first
// run keeps every message, or none with baseline on. The cursor is saved only once every lookup has
// succeeded, so a failed run is retried in full by the next one.
func (s *gmailSince) filter(ctx context.Context, accessToken string, messages []map[string]interface{}) ([]workflow.Item, error) {
	seen := make(map[string]bool, len(s.prev.IDs))
	for _, id := range s.prev.IDs {
		seen[id] = true
	}
	next := gmailCursor{NewestMs: s.prev.NewestMs}
	dates := make([]int64, len(messages))
	for i, msg := range messages {
		id, _ := msg["id"].(string)
		if id == "" {
			return nil, fmt.Errorf("listed message %d has no id", i)
		}
		meta, err := gmailRequest(ctx, "GET", gmailBaseURL+"/messages/"+url.PathEscape(id)+"?format=minimal", accessToken, nil)
		if err != nil {
			return nil, fmt.Errorf("message %s: %w", id, err)
		}
		raw, _ := meta["internalDate"].(string)
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("message %s: internalDate %q is not epoch milliseconds", id, raw)
		}
		dates[i] = ms
		if ms > next.NewestMs {
			next.NewestMs = ms
		}
	}

	items := make([]workflow.Item, 0, len(messages))
	for i, msg := range messages {
		id, _ := msg["id"].(string)
		if dates[i] == next.NewestMs {
			next.IDs = append(next.IDs, id)
		}
		if (s.found && (seen[id] || dates[i] < s.prev.NewestMs)) || (!s.found && s.baseline) {
			continue
		}
		msg["internalDate"] = strconv.FormatInt(dates[i], 10)
		items = append(items, workflow.NewItem(msg))
	}
	if next.NewestMs == s.prev.NewestMs {
		for _, id := range s.prev.IDs {
			if !slices.Contains(next.IDs, id) {
				next.IDs = append(next.IDs, id)
			}
		}
	}
	if err := s.sd.Set(ctx, s.key, next); err != nil {
		return nil, err
	}
	return items, nil
}
