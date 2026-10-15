package n8nimport

import "fmt"

func init() {
	Register("n8n-nodes-base.executeWorkflowTrigger", mapSubWorkflowTrigger)
	Register("n8n-nodes-base.errorTrigger", mapErrorTrigger)
	Register("n8n-nodes-base.workflowTrigger", mapWorkflowTrigger)
	Register("n8n-nodes-base.simulateTrigger", mapManual)
	Register("n8n-nodes-base.timeSaved", mapPassThrough)
	Register("n8n-nodes-base.simulate", mapPassThrough)
	Register("n8n-nodes-base.executionData", mapExecutionData)
	Register("n8n-nodes-base.executeWorkflow", mapExecuteWorkflow)
}

// mapPassThrough maps helper nodes with no effect on the items.
func mapPassThrough(c *MapContext) Mapping {
	for k := range c.Params {
		c.Ignore(k)
	}
	return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
}

// mapSubWorkflowTrigger: the items a parent workflow passes (core.execute_workflow)
// start the run at trigger.execute_workflow, as in n8n.
func mapSubWorkflowTrigger(c *MapContext) Mapping {
	c.Ignore("inputSource", "workflowInputs", "jsonExample")
	if len(c.paramList("workflowInputs.values")) > 0 {
		c.Note("the declared inputs are not checked: the run gets the items the caller passes")
	}
	c.Note("runs when another workflow calls it, or with `monoagentcli workflow run <id> --input '{…}'`")
	return Mapping{Type: "trigger.execute_workflow", Config: map[string]interface{}{}}
}

// mapErrorTrigger: the run starts with the failed execution's
// {execution, workflow} item, as n8n's Error Trigger does.
func mapErrorTrigger(c *MapContext) Mapping {
	c.Note("runs when a workflow whose settings.error_workflow names this workflow fails " +
		"(or for every workflow: monoagentcli config set workflow.error_workflow <id>)")
	return Mapping{Type: "trigger.error", Config: map[string]interface{}{}}
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

// mapExecuteWorkflow: a call of a saved workflow becomes
// core.execute_workflow. n8n's workflow ID means nothing in mono-agent, so
// the node names the sub-workflow by the name n8n cached for it (import
// the sub-workflow under that name), else keeps the ID with a warning.
func mapExecuteWorkflow(c *MapContext) Mapping {
	c.Ignore("operation", "outdatedVersionWarning", "sourceDeprecationNotice", "executeWorkflowNotice")
	if src := c.Literal("source", "database"); src != "database" {
		c.Ignore("workflowPath", "workflowJson", "workflowUrl", "mode", "options", "workflowInputs")
		c.issues = append(c.issues, Issue{Severity: SevError, Param: "source", Value: src,
			Message: "core.execute_workflow runs saved workflows only: import the sub-workflow and pick it"})
		return Mapping{Unmapped: true}
	}
	id, byName := "", false
	switch v := c.Raw("workflowId").(type) {
	case string:
		id = c.Str("workflowId", "")
	case map[string]interface{}:
		if name, _ := v["cachedResultName"].(string); name != "" && v["mode"] == "list" {
			id, byName = name, true
			c.Note("the sub-workflow is found by its n8n name %q: import it under that name", name)
		} else {
			id = fmt.Sprint(c.Translate("workflowId", v))
		}
	}
	if !byName {
		c.Warn("workflowId", c.Raw("workflowId"), "is an n8n workflow ID: set it to the mono-agent ID or name of the imported sub-workflow")
	}
	cfg := map[string]interface{}{"workflowId": id, "mode": c.Literal("mode", "once"),
		"waitForSubWorkflow": c.Bool("options.waitForSubWorkflow", true)}
	m := Mapping{Type: "core.execute_workflow", Config: cfg}
	return withPre(workflowInputsStep(c), m)
}

// workflowInputsStep turns the inputs a call defines (resource mapper,
// "defineBelow") into a core.set that sends only those fields.
func workflowInputsStep(c *MapContext) *ChainStep {
	in := c.paramMap("workflowInputs")
	values, _ := in["value"].(map[string]interface{})
	if in["mappingMode"] != "defineBelow" || len(values) == 0 {
		return nil
	}
	types := map[string]string{}
	schema, _ := in["schema"].([]interface{})
	for _, f := range schema {
		if m, ok := f.(map[string]interface{}); ok {
			types[fmt.Sprint(m["id"])] = map[string]string{"number": "number", "boolean": "bool",
				"array": "json", "object": "json"}[fmt.Sprint(m["type"])]
		}
	}
	list := make([]interface{}, 0, len(values))
	for _, k := range sortedKeys(values) {
		typ := types[k]
		if typ == "" {
			typ = "string"
		}
		v := c.Translate("workflowInputs."+k, setValueString(values[k]))
		list = append(list, map[string]interface{}{"field": k, "value": v, "type": typ})
	}
	return &ChainStep{Type: "core.set", Suffix: " (inputs)",
		Config: map[string]interface{}{"assignments": list, "include_input": false}}
}
