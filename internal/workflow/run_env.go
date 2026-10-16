package workflow

// EnvWorkflowRun marks every process a workflow node starts with the run's
// execution id: what a workflow runs (a daemon's, a chat's or an org's) is
// never the operator, so it can't approve or reject what agents asked for
// (cmd/monoagentcli requireOperator).
const EnvWorkflowRun = "MONOAGENT_WORKFLOW_RUN"

// RunMarker is the value of EnvWorkflowRun for a run: its execution id, or
// "node-run" for a node run outside an execution (`node run`).
func RunMarker(executionID string) string {
	if executionID == "" {
		return "node-run"
	}
	return executionID
}

// RunEnv returns env with EnvWorkflowRun set for the run, appended last so a
// node's own environment settings can't clear it (the last value wins).
func RunEnv(env []string, executionID string) []string {
	return append(env, EnvWorkflowRun+"="+RunMarker(executionID))
}
