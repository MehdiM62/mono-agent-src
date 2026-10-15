// Package profilegate is the CLI's profile gate: every command either acts on
// a profile ("scoped") or on the machine ("global"). A scoped command must be
// told which profile with --profile; the gate judges an invocation before the
// command runs. cmd/monoagentcli installs it.
package profilegate

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/accountgate"
)

const (
	ClassScoped = "scoped"
	ClassGlobal = "global"

	// GroupAnnotation marks a command that only groups subcommands (its own
	// run just prints help or rejects an unknown subcommand). The gate does not
	// judge it.
	GroupAnnotation = "monoagent.profile.group"
)

// classes lists the commands whose class is decided here, keyed by command
// path without the root's name. A command takes the class of its nearest
// listed ancestor (itself included); a command that nothing lists is scoped,
// so a command added later demands --profile until someone decides it acts on
// the machine. A scoped entry carves an exception out of a global group.
//
// Global means the command reads and writes only machine state (templates,
// configs, settings, agent roster, the monoes.me session, package registries,
// service units) and never filters by, or writes, a profile_id.
var classes = map[string]string{
	"version": ClassGlobal, "update": ClassGlobal, "release": ClassGlobal,
	"help": ClassGlobal, "completion": ClassGlobal, "ref": ClassGlobal, "init": ClassGlobal,
	"nodejs": ClassGlobal, "integration": ClassGlobal, "action": ClassGlobal,
	"account": ClassGlobal, "agent": ClassGlobal, "config": ClassGlobal,
	"message-template": ClassGlobal, "db": ClassGlobal,

	// `extension bind <browser> <profile>` takes its profile positionally.
	"extension": ClassGlobal,

	"api config": ClassGlobal,

	"automation":                      ClassGlobal,
	"automation list":                 ClassScoped, // login state of the profile's sessions
	"automation show":                 ClassScoped,
	"automation doctor":               ClassScoped,
	"automation doctor accept-repair": ClassGlobal,
	"automation rerecord":             ClassScoped, // drives the profile's browser

	"profile list": ClassGlobal, "profile create": ClassGlobal, "profile get": ClassGlobal,
	"profile folder": ClassGlobal, "profile move": ClassGlobal, "profile projects": ClassGlobal,
	"profile switch": ClassGlobal, "profile current": ClassGlobal, // removed in a later phase

	"node list": ClassGlobal, "node palette": ClassGlobal, "node schema": ClassGlobal, "node curl": ClassGlobal,

	// library logout deletes the resolved profile's legacy vault token
	// (Client.LogoutLegacy on a VaultStore bound to the profile): scoped.
	"library login": ClassGlobal, "library status": ClassGlobal,

	"workflow templates list": ClassGlobal, "workflow templates show": ClassGlobal,
	"workflow convert": ClassGlobal,

	"secret keyring": ClassGlobal, "secret encrypt-connections": ClassGlobal,

	// chat history reconcile is deliberately cross-profile: it interrupts the
	// active turns of EVERY profile (the desktop app runs it at startup with no
	// profile). Its siblings (list, show, turns, ...) stay scoped.
	"chat history reconcile": ClassGlobal,

	"coder status": ClassGlobal, "coder enable": ClassGlobal, "coder disable": ClassGlobal, "coder set": ClassGlobal,
	// The origin→folder map is the machine-wide coder_projects setting.
	"coder projects": ClassGlobal,

	// The agent allowlist is one machine-wide setting, `browser browsers`
	// lists every attached browser, and `browser diff` only compares two
	// files. Every other browser command drives the profile's browser.
	"browser allow": ClassGlobal, "browser browsers": ClassGlobal, "browser diff": ClassGlobal,
	// What each kind of agent may do in the browser: one machine-wide setting.
	"browser permissions": ClassGlobal,

	"daemon install": ClassGlobal, "daemon uninstall": ClassGlobal, "daemon restart": ClassGlobal,
}

// CommandKey is c's path below the root: "workflow templates list".
func CommandKey(c *cobra.Command) string { return accountgate.CommandKey(c) }

// Class is the class of c: its nearest listed ancestor's, else scoped.
func Class(c *cobra.Command) string {
	for ; c != nil && c.HasParent(); c = c.Parent() {
		if class, ok := classes[CommandKey(c)]; ok {
			return class
		}
	}
	return ClassScoped
}

// Listed is a copy of the table, for the tests that hold it to the real tree.
func Listed() map[string]string {
	out := make(map[string]string, len(classes))
	for k, v := range classes {
		out[k] = v
	}
	return out
}
