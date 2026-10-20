// Package agentmode is `monoagentcli agent-mode`: how to run a coding agent so that
// it cannot read this user's credentials, and use mono-agent through the daemon's
// profile-scoped HTTP API instead of running the CLI as the user.
//
// The OS key stores do not separate one process of a user from another, so only an
// OS boundary keeps an agent away from ~/.monoagent and the key store (SECURITY.md,
// "A copy of ~/.monoagent and the key store"). This package prints the profile of that
// boundary; it reads and writes nothing of the user's.
package agentmode

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Runtime names a sandbox the profile is written for.
type Runtime string

const (
	Bwrap      Runtime = "bwrap"
	Seatbelt   Runtime = "seatbelt"
	ClaudeCode Runtime = "claude-code"
)

// Runtimes lists the supported runtimes, in the order help shows them.
var Runtimes = []Runtime{Bwrap, Seatbelt, ClaudeCode}

// The profiles are fixed text: no path or value of the caller is put into them, so
// nothing can be injected. Paths come from $HOME (bwrap), a -D parameter (seatbelt)
// or ~ (Claude Code) at the time the sandbox starts.

const bwrapProfile = `#!/bin/sh
# monoagentcli agent-mode sandbox --runtime bwrap
# Usage: sh this-script COMMAND [ARGS...]   e.g.  sh this-script claude
# Runs COMMAND with the filesystem writable as usual but with mono-agent's state, the
# Secret Service keyrings and the D-Bus session bus hidden. Network is NOT unshared:
# the agent reaches the daemon's API on loopback (see "agent mode" in SECURITY.md).
# If your session bus is an abstract socket (unix:abstract=...), file masking cannot
# hide it: add --unshare-net and reach the API through a socket you forward in.
set -eu
run="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
exec bwrap \
  --bind / / \
  --dev-bind /dev /dev \
  --tmpfs "$HOME/.monoagent" \
  --tmpfs "$HOME/.local/share/keyrings" \
  --tmpfs "$HOME/.local/share/kwalletd" \
  --tmpfs "$HOME/.gnupg" \
  --ro-bind /dev/null "$run/bus" \
  --tmpfs "$run/keyring" \
  --unsetenv DBUS_SESSION_BUS_ADDRESS \
  --unsetenv GNOME_KEYRING_CONTROL \
  --die-with-parent \
  "$@"
`

const seatbeltProfile = `;; monoagentcli agent-mode sandbox --runtime seatbelt
;; Usage: sandbox-exec -D HOME="$HOME" -f this-file COMMAND [ARGS...]
;; Denies mono-agent's state and the keychain files, and the Mach services that hand out
;; keychain items (which is what /usr/bin/security talks to). Everything else, loopback
;; networking included, stays as it is. Check the service names on your macOS version
;; with: log stream --predicate 'sender == "Sandbox"' while the agent runs.
(version 1)
(allow default)
(deny file-read* file-write*
  (subpath (string-append (param "HOME") "/.monoagent"))
  (subpath (string-append (param "HOME") "/Library/Keychains")))
(deny mach-lookup
  (global-name "com.apple.SecurityServer")
  (global-name "com.apple.securityd")
  (global-name "com.apple.secd"))
`

const claudeCodeProfile = `{
  "_comment": "monoagentcli agent-mode sandbox --runtime claude-code. Merge into .claude/settings.json (or settings.local.json). Check the keys against your Claude Code version.",
  "sandbox": {
    "enabled": true,
    "filesystem": {
      "denyRead": [
        "~/.monoagent",
        "~/Library/Keychains",
        "~/.local/share/keyrings",
        "~/.local/share/kwalletd"
      ]
    }
  },
  "permissions": {
    "deny": [
      "Read(~/.monoagent/**)",
      "Read(~/Library/Keychains/**)",
      "Read(~/.local/share/keyrings/**)"
    ]
  }
}
`

// Profile returns the sandbox profile for rt.
func Profile(rt Runtime) (string, error) {
	switch rt {
	case Bwrap:
		return bwrapProfile, nil
	case Seatbelt:
		return seatbeltProfile, nil
	case ClaudeCode:
		return claudeCodeProfile, nil
	}
	return "", fmt.Errorf("unknown runtime %q (want one of %v)", rt, Runtimes)
}

// NewCommand is `agent-mode`.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-mode",
		Short: "Run a coding agent without your credentials: a sandbox profile, and the scoped HTTP API",
		Long: "Agent mode keeps a coding agent away from ~/.monoagent, the OS key store and the D-Bus session bus, " +
			"and gives it the daemon's HTTP API with one profile's bearer token instead of the CLI run as you. " +
			"See \"Agent mode\" in SECURITY.md.",
	}
	cmd.AddCommand(newSandboxCmd())
	return cmd
}

func newSandboxCmd() *cobra.Command {
	var runtime string
	c := &cobra.Command{
		Use:   "sandbox",
		Short: "Print a sandbox profile that hides mono-agent's state and the key store from an agent",
		Long: "Prints, to stdout, a profile for bwrap (Linux), seatbelt (macOS sandbox-exec) or Claude Code's " +
			"sandbox settings. Nothing is read from or written to your mono-agent data.",
		Example: "  monoagentcli agent-mode sandbox --runtime bwrap > agent-sandbox.sh\n" +
			"  sh agent-sandbox.sh claude",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := Profile(Runtime(runtime))
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), out)
			return err
		},
	}
	c.Flags().StringVar(&runtime, "runtime", string(Bwrap), "sandbox to write the profile for: bwrap, seatbelt or claude-code")
	return c
}
