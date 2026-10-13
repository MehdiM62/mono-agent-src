package control

// ExecuteWorkflowNodeSchema documents the config keys
// ExecuteWorkflowNode.Execute reads out of its map[string]interface{} config —
// see SetNodeSchema's doc comment for why this is a companion struct rather
// than the runtime config, and internal/tools/schemagen for the tag grammar.
type ExecuteWorkflowNodeSchema struct {
	WorkflowID string `json:"workflowId" schema:"label=Workflow,type=text,required,placeholder=workflow ID or name,help=The workflow to run. It need not be active; give it a 'When called by another workflow' trigger to receive the items."`

	Mode string `json:"mode" schema:"label=Mode,type=select,options=once|each,default=once,help=once: one run with all items. each: one run per item."`

	WaitForSubWorkflow bool `json:"waitForSubWorkflow" schema:"label=Wait for Sub-workflow,type=boolean,default=true,help=On: wait and output the sub-workflow's last node items. Off: output the input items at once with the started execution ID."`
}
