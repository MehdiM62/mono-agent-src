package aitasknodes

// TaskNodeSchema documents the config keys TaskNode.Execute reads out of its
// map[string]interface{} config. There is deliberately no budget, unattended
// or start option: the node only queues, and the spend cap comes from the
// operator's AI task budgets.
type TaskNodeSchema struct {
	Goal string `json:"goal" schema:"label=Goal,type=textarea,rows=4,required,help=What the coder is asked to do. The task is queued; a person starts it from the bubble queue."`

	Kind string `json:"kind" schema:"label=Kind,type=text,default=generic,help=The task kind (see aitask --help). Defaults to generic."`

	Context string `json:"context" schema:"label=Context,type=array,item_type=text,help=Each entry is CLASS=PATH with CLASS one of posting; facts; document; selection. Messages and people threads are refused."`

	ApplicationID string `json:"application_id" schema:"label=Application ID,type=text,help=Optional. The application the task is about."`

	Access string `json:"access" schema:"label=Access,type=text,help=Optional. coding; workspace or qa. Blank takes the kind's default."`

	Runtime string `json:"runtime" schema:"label=Runtime,type=text,help=Optional. The agent runtime. Blank takes the default."`

	ProfileID string `json:"profile_id" schema:"label=Profile ID,type=text,help=Optional. Must match the workflow's profile (leave blank to use it)."`
}
