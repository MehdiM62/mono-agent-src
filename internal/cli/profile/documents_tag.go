package profile

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/doctags"
	"github.com/monoes/mono-agent/internal/peopletags"
)

// newProfileDocumentsTagCmd manages the tags on documents. Tags are the
// profile's own, the same ones people carry.
func (d Deps) newProfileDocumentsTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Tag documents: list, add, remove, search",
		Long: "Tags belong to the profile and are matched by name, case-insensitively; they are " +
			"the same tags people carry. A document has at most 10.",
	}
	cmd.AddCommand(d.newDocTagListCmd(), d.newDocTagAddCmd(), d.newDocTagRemoveCmd(), d.newDocTagSearchCmd())
	return cmd
}

func (d Deps) withDocTagDB(fn func(db *sql.DB) error) error {
	db, err := d.OpenDB()
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer db.DB.Close()
	return fn(db.DB)
}

func (d Deps) docTagErr(err error) error {
	switch {
	case errors.Is(err, peopletags.ErrNotFound):
		return d.NotFound("%v", err)
	case errors.Is(err, peopletags.ErrInvalid):
		return d.InvalidInput("%v", err)
	}
	return err
}

func (d Deps) printDocTags(cmd *cobra.Command, tags ...doctags.Tag) error {
	if d.JSONOutput() {
		if len(tags) == 1 {
			return printJSON(tags[0])
		}
		return printJSON(tags)
	}
	if len(tags) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No tags.")
		return nil
	}
	table := d.NewTable(cmd.OutOrStdout(), []string{"ID", "Name", "Color"})
	for _, t := range tags {
		table.Append([]string{t.ID, t.Name, t.Color})
	}
	table.Render()
	return nil
}

func (d Deps) newDocTagListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <doc-id>",
		Short: "List a document's tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.withDocTagDB(func(db *sql.DB) error {
				tags, err := doctags.List(cmd.Context(), db, d.ProfileID(), args[0])
				if err != nil {
					return d.docTagErr(err)
				}
				if d.JSONOutput() {
					return printJSON(tags)
				}
				return d.printDocTags(cmd, tags...)
			})
		},
	}
}

func (d Deps) newDocTagAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add <doc-id> <tag-name>",
		Short:   "Tag a document, creating the tag when new",
		Example: `  monoagentcli profile documents tag add doc-003 "contract"`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.withDocTagDB(func(db *sql.DB) error {
				t, err := doctags.Add(cmd.Context(), db, d.ProfileID(), args[0], args[1])
				if err != nil {
					return d.docTagErr(err)
				}
				return d.printDocTags(cmd, t)
			})
		},
	}
}

func (d Deps) newDocTagRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <doc-id> <tag>",
		Short: "Untag a document (the tag itself stays)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.withDocTagDB(func(db *sql.DB) error {
				if err := doctags.Remove(cmd.Context(), db, d.ProfileID(), args[0], args[1]); err != nil {
					return d.docTagErr(err)
				}
				if d.JSONOutput() {
					return printJSON(map[string]string{"id": args[0], "removed": args[1]})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s.\n", args[1], args[0])
				return nil
			})
		},
	}
}

func (d Deps) newDocTagSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <tag>...",
		Short: "Find documents that carry all the given tags",
		Example: `  monoagentcli profile documents tag search contract
  monoagentcli --json profile documents tag search contract 2026`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.withDocTagDB(func(db *sql.DB) error {
				ids, err := doctags.Search(cmd.Context(), db, d.ProfileID(), args)
				if err != nil {
					return d.docTagErr(err)
				}
				if d.JSONOutput() {
					return printJSON(ids)
				}
				if len(ids) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No documents with "+strings.Join(args, " + ")+".")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(ids, "\n"))
				return nil
			})
		},
	}
}
