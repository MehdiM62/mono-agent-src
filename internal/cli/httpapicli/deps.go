// Package httpapicli is `monoagentcli httpapi`: the REST/JSON HTTP API server (internal/httpapi)
// in the foreground, with the OpenAI-compatible API (internal/cli/apiruntime) in the same
// process. Everything the command needs from the CLI around it comes in through Deps.
package httpapicli

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/apiruntime"
	"github.com/monoes/mono-agent/internal/storage"
)

// Deps is what the command takes from the CLI around it.
type Deps struct {
	// OpenGatewayDB opens (and migrates) the database the OpenAI-compatible API reads its keys
	// and roster from.
	OpenGatewayDB func() (*storage.Database, error)
	// DBPath is the global --db-path flag.
	DBPath func() string
	// Version is the CLI's version, as the server reports it.
	Version func() string
	// API is what the OpenAI-compatible API's runtime takes from the CLI.
	API apiruntime.Deps
	// Serving marks the command as one that serves, under the account gate's serving guard.
	Serving func(*cobra.Command) *cobra.Command
}
