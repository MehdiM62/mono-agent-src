package n8nimport

func init() {
	Register("n8n-nodes-base.executeWorkflowTrigger", mapSubWorkflowTrigger)
	Register("n8n-nodes-base.errorTrigger", mapErrorTrigger)
	Register("n8n-nodes-base.workflowTrigger", mapWorkflowTrigger)
	Register("n8n-nodes-base.simulateTrigger", mapManual)
	Register("n8n-nodes-base.timeSaved", mapPassThrough)
	Register("n8n-nodes-base.simulate", mapPassThrough)
	Register("n8n-nodes-base.executionData", mapExecutionData)
}

// mapPassThrough maps helper nodes with no effect on the items.
func mapPassThrough(c *MapContext) Mapping {
	for k := range c.Params {
		c.Ignore(k)
	}
	return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
}

// mapSubWorkflowTrigger: the inputs a parent workflow passed become the
// run's input.
func mapSubWorkflowTrigger(c *MapContext) Mapping {
	c.Ignore("inputSource", "workflowInputs", "jsonExample")
	c.Note("start it with `monoagentcli workflow run <id> --input '{…}'`; n8n started it from another workflow")
	return Mapping{Type: "trigger.manual", Config: map[string]interface{}{}}
}

func mapErrorTrigger(c *MapContext) Mapping {
	c.Warn("trigger", "errorTrigger", "mono-agent has no error workflows: this workflow now starts manually (handle failures with on-error branches instead)")
	return Mapping{Type: "trigger.manual", Config: map[string]interface{}{}}
}

func mapWorkflowTrigger(c *MapContext) Mapping {
	c.Ignore("events")
	c.Warn("events", c.Raw("events"), "mono-agent has no workflow activation events: this workflow now starts manually")
	return Mapping{Type: "trigger.manual", Config: map[string]interface{}{}}
}

func mapExecutionData(c *MapContext) Mapping {
	c.Ignore("dataToSave")
	if c.Has("dataToSave") {
		c.Warn("dataToSave", c.Raw("dataToSave"), "custom execution data is not saved; the items pass on unchanged")
	}
	return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
}
