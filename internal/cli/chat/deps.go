// Package chat is `monoagentcli chat` (a turn with the user's AI agent, streamed as NDJSON to
// the desktop app and the browser extension, with its history, answers and stops) and
// `monoagentcli coder` (coder mode: chats where the agent has full access to this computer).
// Everything the commands need from the CLI around them comes in through Deps; monomind
// itself (the capability probe, the runtime scan, the workspace init) stays a package
// variable each, so tests can stand in for it.
package chat

import (
	"database/sql"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
)

// Deps is what the commands take from the CLI around them.
type Deps struct {
	// JSONOutput reports the global --json flag; it is read when a command runs.
	JSONOutput func() bool
	// ProfileID is the --profile flag, or the profile OpenDB resolved; it is read when a
	// command runs.
	ProfileID func() string
	// DBPath is the --db-path flag (the chat control files live next to the database).
	DBPath func() string
	// OpenDB opens (and migrates) the database the command is pointed at.
	OpenDB func() (*storage.Database, error)
	// ResolveProfileID turns a profile id or name into its id (exit 3 when there is none).
	ResolveProfileID func(db *sql.DB, idOrName string) (string, error)
	// RequireOperator refuses an agent: only the operator may do what.
	RequireOperator func(what string) error
	// PublicationPrompt is the agents' instruction to register what they publish.
	PublicationPrompt func() string
}

// NewChatCommand is `monoagentcli chat`.
func NewChatCommand(d Deps) *cobra.Command { return newChatCmd(&d) }

// NewCoderCommand is `monoagentcli coder`.
func NewCoderCommand(d Deps) *cobra.Command { return newCoderCmd(&d) }

// NewCoderSetCommand is `monoagentcli coder set`.
func NewCoderSetCommand(d Deps) *cobra.Command { return newCoderSetCmd(&d) }

// CapabilityProbe is monomind's capability handshake, as coder mode runs it.
func CapabilityProbe(cmd *cobra.Command) (*monomind.CapabilitySet, error) {
	return capabilityProbe(cmd)
}

// Settings are the coder-mode settings.
type Settings = coderSettings

// LoadSettings reads the coder-mode settings.
func LoadSettings(db *sql.DB) (Settings, error) { return loadCoderSettings(db) }

// TerminateProcess asks pid to stop (SIGTERM); KillProcess stops it (SIGKILL). Neither is
// supported on Windows.
func TerminateProcess(pid int) error { return terminateProcess(pid) }

// KillProcess stops pid (SIGKILL).
func KillProcess(pid int) error { return killProcess(pid) }
