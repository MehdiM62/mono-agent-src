package monomind

import "os/exec"

// OrgKindEnv vouches for the org kind of agent (browser permissions,
// internal/browserctl.IssueAgentKind) for the role shells an org run
// starts: the environment to give them, and done for when the run ends.
// The binary that has browserctl installs it (browserctl imports this
// package); nil gives none, and the roles count as kind other.
var OrgKindEnv func() (env []string, done func())

// withOrgKind adds OrgKindEnv's environment to cmd, an org launch, and
// returns its done. A detached launch never calls done: the record stops
// counting when this process is gone.
func withOrgKind(cmd *exec.Cmd) (done func()) {
	if OrgKindEnv == nil {
		return func() {}
	}
	env, done := OrgKindEnv()
	cmd.Env = append(cmd.Env, env...)
	return done
}
