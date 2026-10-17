package profile

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/operator"
)

// refuseNotOperator refuses changing or deleting a profile to anyone but the
// user: a profile holds their accounts, credentials and data, and a chat, a
// workflow run or an agent must not be able to rename it or erase it.
func (d Deps) refuseNotOperator(what string) error {
	if why := operator.Refusal(); why != "" {
		return d.InvalidInput("only the user %s a profile (%s); the user runs this in their own terminal or in the app's Settings: %s", what, "it holds their accounts and data", why)
	}
	return nil
}

// profileTable is one table that holds rows of a profile, with how many.
type profileTable struct {
	Table string `json:"table"`
	Rows  int    `json:"rows"`
}

// profileTables lists every table with a profile_id column and the number
// of rows the profile has in it (tables where it has none are left out).
// The profiles table itself has no profile_id column, so it is not here.
func profileTables(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) ([]profileTable, error) {
	rows, err := q.QueryContext(ctx, `SELECT m.name FROM sqlite_master m
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		  AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) WHERE name = 'profile_id')`)
	if err != nil {
		return nil, fmt.Errorf("list profile tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []profileTable
	for _, n := range names {
		var c int
		// n comes from sqlite_master, not from the user.
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+n+`" WHERE profile_id = ?`, id).Scan(&c); err != nil {
			return nil, fmt.Errorf("count %s: %w", n, err)
		}
		if c > 0 {
			out = append(out, profileTable{Table: n, Rows: c})
		}
	}
	return out, nil
}

type profileImpact struct {
	Profile profileJSON    `json:"profile"`
	Tables  []profileTable `json:"tables"`
	// FolderKept says the profile's folder on disk is never deleted.
	FolderKept bool `json:"folder_kept"`
	// Backup is the database snapshot taken before a delete ("" for --summary).
	Backup string `json:"backup,omitempty"`
}

func (d Deps) newProfileUpdateCmd() *cobra.Command {
	var name, icon string
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Rename a profile or change its icon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.refuseNotOperator("renames or re-icons"); err != nil {
				return err
			}
			nameSet, iconSet := cmd.Flags().Changed("name"), cmd.Flags().Changed("icon")
			if !nameSet && !iconSet {
				return d.InvalidInput("nothing to update: pass --name and/or --icon")
			}
			name = strings.TrimSpace(name)
			if nameSet && name == "" {
				return d.InvalidInput("profile name cannot be empty")
			}
			db, err := d.OpenDB()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := d.lookupProfile(db.DB, args[0])
			if err != nil {
				return err
			}
			if nameSet {
				var other string
				err := db.DB.QueryRow(`SELECT id FROM profiles WHERE LOWER(name) = LOWER(?) AND id <> ?`, name, p.ID).Scan(&other)
				if err == nil {
					return d.InvalidInput("a profile named %q already exists", name)
				}
				if err != sql.ErrNoRows {
					return fmt.Errorf("check profile name: %w", err)
				}
				p.Name = name
			}
			if iconSet {
				p.Icon = strings.TrimSpace(icon)
			}
			if _, err := db.DB.Exec(`UPDATE profiles SET name = ?, icon = ? WHERE id = ?`, p.Name, p.Icon, p.ID); err != nil {
				return fmt.Errorf("update profile: %w", err)
			}
			if d.JSONOutput() {
				return printJSON(p)
			}
			fmt.Printf("Updated profile: %s (%s)\n", p.Name, p.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New profile name")
	cmd.Flags().StringVar(&icon, "icon", "", "Icon id from the agent-avatars manifest (empty clears it)")
	return cmd
}

func (d Deps) newProfileDeleteCmd() *cobra.Command {
	var summary bool
	var confirmName string
	cmd := &cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Delete a profile and everything stored under it",
		Long: "Delete a profile together with its rows in every profile-scoped table (people, workflows, " +
			"credentials, tasks, chats, …). The profile's folder on disk is left untouched. " +
			"--summary only prints what would be deleted. Deleting needs --yes-delete-profile <the profile's exact name>, " +
			"takes a database snapshot first (printed, under the backups folder) and only the user can run it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !summary {
				if err := d.refuseNotOperator("deletes"); err != nil {
					return err
				}
				if confirmName == "" {
					return d.InvalidInput("deleting a profile is permanent: pass --yes-delete-profile <its exact name> (or --summary to see what it would remove)")
				}
			}
			db, err := d.OpenDB()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := d.lookupProfile(db.DB, args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			tables, err := profileTables(ctx, db.DB, p.ID)
			if err != nil {
				return err
			}
			impact := profileImpact{Profile: *p, Tables: tables, FolderKept: true}
			if summary {
				if d.JSONOutput() {
					return printJSON(impact)
				}
				fmt.Printf("Deleting %s would remove:\n", p.Name)
				for _, t := range tables {
					fmt.Printf("  %-32s %d\n", t.Table, t.Rows)
				}
				fmt.Printf("Its folder %s would be left on disk.\n", p.RootDir)
				return nil
			}
			if confirmName != p.Name {
				return d.InvalidInput("--yes-delete-profile %q does not match the profile's name %q", confirmName, p.Name)
			}
			var n int
			if err := db.DB.QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&n); err != nil {
				return fmt.Errorf("count profiles: %w", err)
			}
			if n <= 1 {
				return d.InvalidInput("cannot delete the only profile")
			}
			impact.Backup, err = db.SnapshotBeforeProfileDelete(ctx)
			if err != nil {
				return fmt.Errorf("backing up the database before deleting (nothing was deleted): %w", err)
			}
			if err := deleteProfile(ctx, db.DB, p.ID, tables); err != nil {
				return err
			}
			if d.JSONOutput() {
				return printJSON(impact)
			}
			fmt.Printf("Deleted profile: %s (%s)\nDatabase backup: %s\nIts folder %s was left on disk.\n", p.Name, p.ID, impact.Backup, p.RootDir)
			return nil
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Only print what deleting would remove")
	cmd.Flags().StringVar(&confirmName, "yes-delete-profile", "", "Confirm the deletion by giving the profile's exact name")
	return cmd
}

// deleteProfile removes the profile's rows from tables and then the profile,
// all or nothing. If it was the active profile, the setting is cleared.
func deleteProfile(ctx context.Context, db *sql.DB, id string, tables []profileTable) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Rows in child tables that reference these through their own keys are
	// checked at commit, after the cascades have run.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "`+t.Table+`" WHERE profile_id = ?`, id); err != nil {
			return fmt.Errorf("delete from %s: %w", t.Table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'active_profile_id' AND value = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}
