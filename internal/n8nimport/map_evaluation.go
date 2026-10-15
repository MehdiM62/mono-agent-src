package n8nimport

import "fmt"

// n8n's evaluations: the Evaluation Trigger ("When fetching a dataset row")
// becomes trigger.evaluation, which `monoagentcli workflow evaluate` fires
// once per dataset row, and the Evaluation node core.evaluation.

func init() {
	Register("n8n-nodes-base.evaluationTrigger", mapEvaluationTrigger)
	Register("n8n-nodes-base.evaluation", mapEvaluation)
}

// evaluationOps are the Evaluation node's operations; n8n's node fails any
// other with "Unsupported Operation".
var evaluationOps = map[string]bool{"setInputs": true, "setOutputs": true, "setMetrics": true, "checkIfEvaluating": true}

// evaluationSource is the dataset source of an evaluation trigger or
// setOutputs node: data tables are the default from typeVersion since.
func evaluationSource(c *MapContext, since float64) string {
	def := "googleSheets"
	if c.version(since) {
		def = "dataTable"
	}
	return c.Literal("source", def)
}

// evaluationSheet sets the Google Sheet of a googleSheets dataset.
func evaluationSheet(c *MapContext, cfg map[string]interface{}) {
	cfg["credential_id"] = c.Credential("google_sheets", "googleSheetsOAuth2Api", "googleApi")
	cfg["spreadsheet_id"] = c.rlValue("documentId")
	cfg["sheet_name"] = c.rlValue("sheetName")
	c.Ignore("authentication", "credentials")
}

func mapEvaluationTrigger(c *MapContext) Mapping {
	c.Ignore("notice")
	cfg := map[string]interface{}{"source": evaluationSource(c, 4.7)}
	if c.Bool("limitRows", false) {
		cfg["limit_rows"] = true
		cfg["max_rows"] = c.Num("maxRows", 10)
	} else {
		c.Ignore("maxRows")
	}
	switch cfg["source"] {
	case "googleSheets":
		evaluationSheet(c, cfg)
		var filters []interface{}
		for _, f := range c.paramList("filtersUI.values") {
			filters = append(filters, map[string]interface{}{"column": fmt.Sprint(f["lookupColumn"]), "condition": "eq",
				"value": c.Translate("filtersUI", f["lookupValue"])})
		}
		c.Ignore("filtersUI", "dataTableId", "filterRows", "matchType", "filters")
		if filters != nil {
			cfg["match"], cfg["filters"] = "all", filters
		}
	case "dataTable":
		cfg["table"] = dataTableRef(c)
		c.Ignore("documentId", "sheetName", "filtersUI", "authentication", "credentials")
		if c.Bool("filterRows", false) {
			cfg["match"] = map[string]string{"allConditions": "all"}[c.Literal("matchType", "anyCondition")]
			if cfg["match"] == "" {
				cfg["match"] = "any"
			}
			var filters []interface{}
			for _, f := range c.paramList("filters.conditions") {
				cond, _ := f["condition"].(string)
				if cond == "" {
					cond = "eq"
				}
				m := map[string]interface{}{"column": fmt.Sprint(f["keyName"]), "condition": cond}
				if v, ok := f["keyValue"]; ok {
					m["value"] = c.Translate("filters", v)
				}
				filters = append(filters, m)
			}
			if filters != nil {
				cfg["filters"] = filters
			}
		}
		c.Ignore("filters", "matchType")
	default:
		c.issues = append(c.issues, Issue{Severity: SevError, Param: "source", Value: cfg["source"],
			Message: fmt.Sprintf("unknown dataset source %q", cfg["source"])})
		return Mapping{Unmapped: true}
	}
	c.Note("run the evaluation with monoagentcli workflow evaluate <workflow-id>: each dataset row runs the workflow once; a manual run leaves this trigger idle")
	return Mapping{Type: "trigger.evaluation", Config: cfg}
}

func mapEvaluation(c *MapContext) Mapping {
	c.Ignore("resource", "notice", "setInputsNotice")
	op := c.Literal("operation", "setOutputs")
	if !evaluationOps[op] {
		for k := range c.Params {
			c.Ignore(k)
		}
		c.issues = append(c.issues, Issue{Severity: SevError, Param: "operation", Value: op,
			Message: fmt.Sprintf("%q is not an Evaluation operation (n8n's node fails it as unsupported)", op)})
		return Mapping{Unmapped: true}
	}
	cfg := map[string]interface{}{"operation": op}
	switch op {
	case "checkIfEvaluating":
		return Mapping{Type: "core.evaluation", Config: cfg, Outputs: []string{"evaluation", "normal"}}
	case "setInputs":
		cfg["inputs"] = evaluationPairs(c, "inputs", "inputName", "inputValue")
	case "setOutputs":
		cfg["source"] = evaluationSource(c, 4.8)
		if cfg["source"] == "googleSheets" {
			evaluationSheet(c, cfg)
			c.Ignore("dataTableId")
		} else {
			cfg["table"] = dataTableRef(c)
			c.Ignore("documentId", "sheetName", "authentication", "credentials")
		}
		cfg["outputs"] = evaluationPairs(c, "outputs", "outputName", "outputValue")
	case "setMetrics":
		mapEvaluationMetric(c, cfg)
	}
	return Mapping{Type: "core.evaluation", Config: cfg}
}

// evaluationPairs reads a fixedCollection of name/value entries into
// [{name, value}].
func evaluationPairs(c *MapContext, param, nameKey, valueKey string) []interface{} {
	out := []interface{}{}
	for _, e := range c.paramList(param + ".values") {
		out = append(out, map[string]interface{}{"name": fmt.Sprint(valueOrEmpty(e[nameKey])),
			"value": c.Translate(param, e[valueKey])})
	}
	c.Ignore(param)
	return out
}

func valueOrEmpty(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	return v
}

// mapEvaluationMetric maps setMetrics: the metric (only custom metrics
// before typeVersion 4.7), its fields and, for the model-judged metrics,
// the attached chat model as runtime and model.
func mapEvaluationMetric(c *MapContext, cfg map[string]interface{}) {
	metric := "customMetrics"
	if c.version(4.7) {
		metric = c.Literal("metric", "correctness")
	} else {
		c.Ignore("metric")
	}
	cfg["metric"] = metric
	fields := map[string]string{"expectedAnswer": "expected_answer", "actualAnswer": "actual_answer",
		"userQuery": "user_query", "expectedTools": "expected_tools", "intermediateSteps": "intermediate_steps",
		"prompt": "prompt", "options.metricName": "metric_name", "options.inputPrompt": "input_prompt"}
	for from, to := range fields {
		if c.Has(from) {
			cfg[to] = c.Value(from)
		}
	}
	if metric == "customMetrics" {
		var list []interface{}
		for _, a := range c.paramList("metrics.assignments") {
			list = append(list, map[string]interface{}{"name": fmt.Sprint(valueOrEmpty(a["name"])),
				"value": c.Translate("metrics", a["value"])})
		}
		c.Ignore("metrics")
		if list == nil {
			list = []interface{}{}
		}
		cfg["metrics"] = list
	}
	needed := metric == "correctness" || metric == "helpfulness"
	found := false
	for _, s := range c.SubNodes {
		if s.Kind != "ai_languageModel" || found {
			c.Warn(s.Kind, s.Name, "sub-node %s is not used by core.evaluation", shortType(s.Type))
			continue
		}
		found = true
		if !needed {
			c.Note("the chat model %s is not needed: the %s metric is computed locally", shortType(s.Type), metric)
			continue
		}
		cfg["runtime"] = "claude"
		useModel(c, cfg, s)
	}
	if needed && !found {
		cfg["runtime"] = "claude"
		c.Note("no chat model is attached; the %s metric is scored with the claude runtime", metric)
	}
}
