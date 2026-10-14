package main

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/documentscli"
	"github.com/monoes/mono-agent/internal/storage"
)

// `documents` lives in internal/cli/documentscli; this is its adapter.

func newDocumentsCmd(cfg *globalConfig) *cobra.Command {
	return documentscli.NewCommand(documentscli.Deps{
		JSONOutput: func() bool { return cfg.JSONOutput },
		ProfileID:  func() string { return cfg.ProfileID },
		OpenDB:     func() (*storage.Database, error) { return initDB(cfg) },
	})
}
