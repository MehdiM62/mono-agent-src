package workflow

// DisabledNodesSetting is the Workflow.Settings key choosing what a
// disabled node does with its input. "passthrough" hands the input to the
// node's main successors unchanged, as n8n does; anything else keeps the
// default: the node is skipped and emits nothing.
const DisabledNodesSetting = "disabled_nodes"

// passDisabled routes a disabled node's input to its main successors when
// the workflow sets disabled_nodes to "passthrough". Trigger nodes never
// pass anything: a disabled trigger does not fire.
func (r *runner) passDisabled(node WorkflowNode) {
	if mode, _ := r.wf.Settings[DisabledNodesSetting].(string); mode != "passthrough" || r.triggerNodeIDs[node.ID] {
		return
	}
	items := r.pendingInputs[node.ID]
	for _, succ := range r.dag.SuccessorsOnHandle(node.ID, "main") {
		r.route(node, succ.ID, items)
	}
	r.nodeOutputs[node.Name] = items
}
