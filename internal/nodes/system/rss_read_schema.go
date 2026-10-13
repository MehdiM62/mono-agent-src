package system

// RSSReadNodeSchema documents the config keys RSSReadNode.Execute reads out
// of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// This matched the hand-written schemas/system.rss_read.json field-for-field;
// since_last_run was added after.
type RSSReadNodeSchema struct {
	URL string `json:"url" schema:"label=RSS/Atom Feed URL,type=text,required,placeholder=https://feeds.example.com/rss.xml"`

	Limit float64 `json:"limit" schema:"label=Max Items,type=number,default=20"`

	SinceLastRun bool `json:"since_last_run" schema:"label=Only New Since Last Run,type=boolean,default=false,help=Emit only entries this node has not seen in an earlier run: a new guid/link not dated before the newest entry already seen. The first run emits the whole feed. Remembered per node and feed URL; Max Items applies after this filter and entries it cuts are not emitted later."`
}
