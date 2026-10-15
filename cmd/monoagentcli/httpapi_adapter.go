package main

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/httpapicli"
	"github.com/monoes/mono-agent/internal/storage"
)

// `httpapi` lives in internal/cli/httpapicli; this is its adapter: the CLI's globals and helpers
// handed over as httpapicli.Deps.

// openGatewayDB opens the database for the OpenAI-compatible API on a copy of
// the configuration, so that resolving a profile name in it leaves cfg alone.
func openGatewayDB(cfg *globalConfig) (*storage.Database, error) {
	c := *cfg
	return initDB(&c)
}

// newHTTPAPICmd runs the REST/JSON HTTP API server in the foreground (httpapicli.NewCommand).
func newHTTPAPICmd(cfg *globalConfig) *cobra.Command {
	return httpapicli.NewCommand(httpapicli.Deps{
		OpenGatewayDB: func() (*storage.Database, error) { return openGatewayDB(cfg) },
		DBPath:        func() string { return cfg.DBPath },
		Version:       func() string { return version },
		API:           apiRuntimeDeps(),
		Serving:       func(c *cobra.Command) *cobra.Command { return servingCommand(c) },
	})
}
