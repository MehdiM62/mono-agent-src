package system

import (
	"context"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/monoes/mono-agent/internal/workflow"
)

// rssCursorMaxIDs bounds the entry ids a cursor keeps (a feed's own length
// is usually far below it).
const rssCursorMaxIDs = 1000

// rssCursor is what a since_last_run node remembers about the previous fetch
// of a feed: the newest entry date it saw and the ids of every entry in it.
type rssCursor struct {
	Newest time.Time `json:"newest"`
	IDs    []string  `json:"ids"`
}

// rssItemID identifies an entry: guid, else link, else title plus date.
func rssItemID(fi *gofeed.Item) string {
	switch {
	case fi.GUID != "":
		return fi.GUID
	case fi.Link != "":
		return fi.Link
	default:
		return fi.Title + "\x00" + fi.Published
	}
}

func rssItemDate(fi *gofeed.Item) *time.Time {
	if fi.PublishedParsed != nil {
		return fi.PublishedParsed
	}
	return fi.UpdatedParsed
}

// itemsSinceLastRun returns the entries of items that the node's previous run
// of feedURL did not see, then stores the cursor for the next run. An entry
// is new when its id was not in the previous fetch and it is not dated before
// the newest entry seen so far. The first run returns every entry. The cursor
// is per node and per feed URL, so changing the URL starts over.
func itemsSinceLastRun(ctx context.Context, input workflow.NodeInput, feedURL string, items []*gofeed.Item) ([]*gofeed.Item, error) {
	sd, err := workflow.StaticDataFor(ctx, input, workflow.StateScopeNode)
	if err != nil {
		return nil, err
	}
	key := "rss_cursor:" + feedURL
	var prev rssCursor
	found, err := sd.Get(ctx, key, &prev)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(prev.IDs))
	for _, id := range prev.IDs {
		seen[id] = true
	}

	next := rssCursor{Newest: prev.Newest}
	fresh := make([]*gofeed.Item, 0, len(items))
	for _, fi := range items {
		id := rssItemID(fi)
		date := rssItemDate(fi)
		if len(next.IDs) < rssCursorMaxIDs {
			next.IDs = append(next.IDs, id)
		}
		if date != nil && date.After(next.Newest) {
			next.Newest = *date
		}
		if found && (seen[id] || (date != nil && date.Before(prev.Newest))) {
			continue
		}
		fresh = append(fresh, fi)
	}
	if err := sd.Set(ctx, key, next); err != nil {
		return nil, err
	}
	return fresh, nil
}
