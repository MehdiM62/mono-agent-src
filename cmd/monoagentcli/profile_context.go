package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/profilectx"
)

func newProfileContextCmd(cfg *globalConfig) *cobra.Command {
	var recent int
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Describe the active profile: folder, accounts, and what it holds",
		Long: "Prints what an agent needs to know about the active profile (or --profile): its name, id and " +
			"folder, connected accounts (platform and handle), counts and a few recent items of tasks, documents, " +
			"people, workflows, orgs and vault items (names only), and the browser extension's status. " +
			"Secrets, tokens and vault values are never included. The same block is put in coder chats' system prompts.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := initDB(cfg)
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.Close()
			c, err := profilectx.Build(cmd.Context(), db.DB, cfg.ProfileID, profilectx.Options{Recent: recent})
			if err != nil {
				return err
			}
			if cfg.JSONOutput {
				return printJSON(c)
			}
			fmt.Fprint(cmd.OutOrStdout(), c.Block())
			return nil
		},
	}
	cmd.Flags().IntVar(&recent, "recent", profilectx.DefaultRecent, "Recent items listed per section")
	withJSONErrors(cfg, cmd)
	return cmd
}
