package monomind

import "os/exec"

// OrgKindEnv vouches for the org kind of agent (browser permissions,
// internal/browserctl.IssueAgentKind) for the role shells an org run
// starts: the environment to give them, bind for a detached launch's pid,
// and done for when the run ends. The binary that has browserctl installs
// it (browserctl imports this package); nil gives none, and the roles
// count as kind other.
var OrgKindEnv func() (env []string, bind func(pid int), done func())

// withOrgKind adds OrgKindEnv's environment to cmd, an org launch, and
// returns its bind and done. A foreground launch calls done when the run
// ends. A detached one calls bind with the started process's pid and never
// done: the record then counts while that process runs, after this one
// has exited.
func withOrgKind(cmd *exec.Cmd) (bind func(pid int), done func()) {
	if OrgKindEnv == nil {
		return func(int) {}, func() {}
	}
	env, bind, done := OrgKindEnv()
	cmd.Env = append(cmd.Env, env...)
	return bind, done
}
