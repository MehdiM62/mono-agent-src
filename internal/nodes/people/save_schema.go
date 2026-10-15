package peoplenodes

// PeopleSaveNodeSchema documents the config keys PeopleSaveNode.Execute
// reads out of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// "profile_id" has no entry in the hand-written schemas/people.save.json
// this replaces — Execute has read it since it
// was written, but the schema only ever exposed "platform". Generating from
// the struct surfaces that gap.
type PeopleSaveNodeSchema struct {
	Platform string `json:"platform" schema:"label=Platform Override,type=select,options=|linkedin|instagram|x|tiktok,default=,help=Force a specific platform. Leave blank to use the platform field from each input item."`

	ProfileID string `json:"profile_id" schema:"label=Profile ID,type=text,help=Optional. Must match the workflow's profile (leave blank to use it)."`
}
