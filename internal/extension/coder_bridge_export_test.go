package extension

// CoderAgentEnv and CoderTurnEnv let the external test hold the bridge's
// lists equal to browserctl.AgentContextEnv and operator.MarkerEnv (imports
// this package can't make).
var (
	CoderAgentEnv = coderAgentEnv
	CoderTurnEnv  = coderTurnEnv
)
