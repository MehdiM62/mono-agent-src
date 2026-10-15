package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// browser allow list · add · remove — the origins an agent may drive
// besides local and dev pages. Only the user changes them.

func newBrowserAllowCmd(cfg *globalConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allow",
		Short: "The sites agents may drive besides local and dev pages",
		Long: "Agents drive local and dev pages by default. Add an origin (https://example.com,\n" +
			"http://staging.example.com:8080) or a wildcard (*.example.com: the domain and every\n" +
			"subdomain) to let them drive it too; a form submit there still waits for your\n" +
			"approval. Only you can change the list: an agent is refused.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List the allowed origins",
		Example: "  monoagentcli --json browser allow list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBrowserAllowlist(cfg, func(list []string) ([]string, error) { return nil, nil }, func(list []string) error {
				return browserOut(cmd, cfg, map[string]any{"origins": list}, func() {
					if len(list) == 0 {
						fmt.Fprintln(cmd.OutOrStdout(), "No origins allowed besides local and dev pages.")
					}
					for _, o := range list {
						fmt.Fprintln(cmd.OutOrStdout(), o)
					}
				})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "add <origin>",
		Short:   "Let agents drive an origin (scheme://host[:port] or *.host)",
		Example: "  monoagentcli browser allow add https://staging.example.com\n  monoagentcli browser allow add '*.example.com'",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if notOperator() != "" {
				return operatorOnlyError("an agent may not change which sites agents drive: ask the user to run `monoagentcli browser allow add %s` in their own terminal", args[0])
			}
			entry, err := browserctl.NormalizeAllowEntry(args[0])
			if err != nil {
				return errInvalidInput("%v", err)
			}
			return withBrowserAllowlist(cfg, func(list []string) ([]string, error) { return append(list, entry), nil }, func(list []string) error {
				return browserOut(cmd, cfg, map[string]any{"origins": list, "added": entry}, func() {
					fmt.Fprintf(cmd.OutOrStdout(), "Agents may now drive %s\n", entry)
				})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "remove <origin>",
		Short:   "Stop agents driving an origin",
		Example: "  monoagentcli browser allow remove https://staging.example.com",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if notOperator() != "" {
				return operatorOnlyError("an agent may not change which sites agents drive: ask the user to run `monoagentcli browser allow remove %s` in their own terminal", args[0])
			}
			entry, err := browserctl.NormalizeAllowEntry(args[0])
			if err != nil {
				return errInvalidInput("%v", err)
			}
			return withBrowserAllowlist(cfg, func(list []string) ([]string, error) {
				out := []string{}
				for _, o := range list {
					if o != entry {
						out = append(out, o)
					}
				}
				if len(out) == len(list) {
					return nil, errNotFound("%s is not on the list", entry)
				}
				return out, nil
			}, func(list []string) error {
				return browserOut(cmd, cfg, map[string]any{"origins": list, "removed": entry}, func() {
					fmt.Fprintf(cmd.OutOrStdout(), "Agents no longer drive %s\n", entry)
				})
			})
		},
	})
	return cmd
}

// withBrowserAllowlist loads the allowlist, lets change return a new one
// (nil: no change) to save, and hands the result to done.
func withBrowserAllowlist(cfg *globalConfig, change func([]string) ([]string, error), done func([]string) error) error {
	db, err := initDB(cfg)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer db.Close()
	list, err := browserctl.LoadAllowlist(db.DB)
	if err != nil {
		return err
	}
	next, err := change(list)
	if err != nil {
		return err
	}
	if next != nil {
		if err := browserctl.SaveAllowlist(db.DB, next); err != nil {
			return err
		}
		if list, err = browserctl.LoadAllowlist(db.DB); err != nil {
			return err
		}
	}
	return done(list)
}
