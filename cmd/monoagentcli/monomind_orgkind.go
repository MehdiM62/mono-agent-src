package main

import (
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/monomind"
)

// The org runs this binary launches vouch for their role shells' kind of
// agent, org (browser permissions); monomind can't import browserctl, which
// imports it.
func init() {
	monomind.OrgKindEnv = func() ([]string, func()) { return browserctl.IssueAgentKind(browserctl.KindOrg) }
}
