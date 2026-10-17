package profile

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/workflow"
)

// refuseNotOperator refuses changing or deleting a profile to anyone but the
// user: a profile holds their accounts and data, and a chat, a workflow run
// or an agent must not be able to rename it or erase it.
func (d Deps) refuseNotOperator(what string) error {
	if why := operator.Refusal(); why != "" {
		return d.InvalidInput("only the user %s a profile (it holds their accounts and data); the user runs this in their own terminal or in the app's Settings: %s", what, why)
	}
	return nil
}

// profileTable is one table that holds rows of a profile, with how many.
type profileTable struct {
	Table string `json:"table"`
	Rows  int    `json:"rows"`
}

type profileImpact struct {
	Profile profileJSON    `json:"profile"`
	Tables  []profileTable `json:"tables"`
	// FolderKept says the profile's folder on disk is never deleted.
	FolderKept bool `json:"folder_kept"`
	// Backup is the database snapshot taken before a delete ("" for --summary).
	// It holds every profile's data and is never pruned.
	Backup string `json:"backup,omitempty"`
	// KeyRemoved says the profile's vault key left the OS keychain.
	KeyRemoved bool `json:"key_removed,omitempty"`
	// Warnings are steps after the delete that did not finish.
	Warnings []string `json:"warnings,omitempty"`
}

const (
	maxProfileNameRunes = 64
	// backupNote is said wherever a backup path is.
	backupNote = "It is a full copy of the database (every profile, not only this one), it is not encrypted, " +
		"and it is never deleted automatically: `monoagentcli profile backups purge` removes these copies."
)

var iconID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// validateProfileName rejects names that are empty, long, hold control or
// bidirectional-control characters or path separators, or that spell another
// profile's id (an id wins over a name when a profile is looked up).
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if utf8.RuneCountInString(name) > maxProfileNameRunes {
		return fmt.Errorf("profile name is longer than %d characters", maxProfileNameRunes)
	}
	for _, r := range name {
		switch {
		case unicode.IsControl(r):
			return fmt.Errorf("profile name cannot contain control characters")
		case r == '/' || r == '\\':
			return fmt.Errorf("profile name cannot contain / or \\")
		case r == '‎' || r == '‏' || r == '؜' || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩'):
			return fmt.Errorf("profile name cannot contain text-direction control characters")
		}
	}
	return nil
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
			name, icon = strings.TrimSpace(name), strings.TrimSpace(icon)
			if nameSet {
				if err := validateProfileName(name); err != nil {
					return d.InvalidInput("%v", err)
				}
			}
			if iconSet && icon != "" && !iconID.MatchString(icon) {
				return d.InvalidInput("icon must be an id from the avatar manifest (lowercase letters, digits, - and _)")
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
				err := db.DB.QueryRow(`SELECT id FROM profiles WHERE id <> ? AND (LOWER(name) = LOWER(?) OR LOWER(id) = LOWER(?))`, p.ID, name, name).Scan(&other)
				if err == nil {
					return d.InvalidInput("%q is already the name or id of another profile", name)
				}
				if err != sql.ErrNoRows {
					return fmt.Errorf("check profile name: %w", err)
				}
				p.Name = name
			}
			if iconSet {
				p.Icon = icon
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

// workflowFilesDir is where workflows are kept as files.
func workflowFilesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".monoagent", "workflows"), nil
}

// profileWorkflowFiles are the ids of the profile's workflow files: those
// whose file says it is the profile's, and those of the workflows the
// database has for it (a file can exist for a workflow the database lost).
func profileWorkflowFiles(ctx context.Context, profileID string, dbIDs []string) (*workflow.WorkflowFileStore, []string, error) {
	dir, err := workflowFilesDir()
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, nil, nil
	}
	fs, err := workflow.NewWorkflowFileStore(dir)
	if err != nil {
		return nil, nil, err
	}
	listed, err := fs.ListWorkflows(ctx)
	if err != nil {
		return nil, nil, err
	}
	ids := map[string]bool{}
	for _, w := range listed {
		if w.ProfileID == profileID {
			ids[w.ID] = true
		}
	}
	for _, id := range dbIDs {
		if _, statErr := os.Stat(filepath.Join(dir, id+".json")); statErr == nil {
			ids[id] = true
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return fs, out, nil
}

func (d Deps) newProfileDeleteCmd() *cobra.Command {
	var summary bool
	var confirmName string
	cmd := &cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Delete a profile and everything stored under it",
		Long: "Delete a profile with its rows in every profile-scoped table (people, messages, workflows, tasks, " +
			"publications, vault items, documents, credentials, chats, ...), its workflow files, its settings entries " +
			"and its vault key in the OS keychain. The profile's folder on disk is kept. A database snapshot is taken " +
			"first. --summary only prints what would be deleted. Deleting needs --yes-delete-profile <the profile's " +
			"exact name>, only the user can run it, and it is refused while the profile has running workflows.",
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
			if summary {
				return d.printDeleteSummary(ctx, db.DB, p)
			}
			if confirmName != p.Name {
				return d.InvalidInput("--yes-delete-profile %q does not match the profile's name %q", confirmName, p.Name)
			}
			var n int
			if err := db.DB.QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&n); err != nil {
				return fmt.Errorf("count profiles: %w", err)
			}
			if n <= 1 {
				return d.InvalidInput("cannot delete the only profile: create another one first")
			}
			if err := refuseRunning(ctx, db.DB, p.ID); err != nil {
				return d.InvalidInput("%v", err)
			}
			impact := profileImpact{Profile: *p, FolderKept: true}
			impact.Backup, err = db.SnapshotBeforeProfileDelete(ctx)
			if err != nil {
				return fmt.Errorf("backing up the database before deleting (nothing was deleted): %w", err)
			}
			wfIDs, tables, err := deleteProfile(ctx, db.DB, p.ID)
			if err != nil {
				return err
			}
			impact.Tables = tables
			// After the commit: files and the keychain are not part of the transaction.
			if fs, ids, ferr := profileWorkflowFiles(ctx, p.ID, wfIDs); ferr != nil {
				impact.Warnings = append(impact.Warnings, "workflow files were not removed: "+ferr.Error())
			} else {
				for _, id := range ids {
					if err := fs.DeleteWorkflow(ctx, id); err != nil {
						impact.Warnings = append(impact.Warnings, fmt.Sprintf("workflow file %s was not removed: %v", id, err))
					}
				}
			}
			if err := secrets.DeleteProfileKEK(p.ID); err != nil {
				impact.Warnings = append(impact.Warnings, "the vault key stays in the OS keychain: "+err.Error())
			} else {
				impact.KeyRemoved = true
			}
			if d.JSONOutput() {
				return printJSON(impact)
			}
			fmt.Printf("Deleted profile: %s (%s)\nDatabase backup: %s\n%s\nIts folder %s was left on disk.\n", p.Name, p.ID, impact.Backup, backupNote, p.RootDir)
			for _, w := range impact.Warnings {
				fmt.Printf("warning: %s\n", w)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Only print what deleting would remove")
	cmd.Flags().StringVar(&confirmName, "yes-delete-profile", "", "Confirm the deletion by giving the profile's exact name")
	return cmd
}

func (d Deps) printDeleteSummary(ctx context.Context, db *sql.DB, p *profileJSON) error {
	scope, err := profileScope(ctx, db)
	if err != nil {
		return err
	}
	tables, err := countScope(ctx, db, scope, p.ID)
	if err != nil {
		return err
	}
	wfIDs, _ := queryStrings(ctx, db, `SELECT id FROM workflows WHERE profile_id = ?`, p.ID)
	if _, files, err := profileWorkflowFiles(ctx, p.ID, wfIDs); err == nil && len(files) > 0 {
		tables = append(tables, profileTable{Table: "workflow files", Rows: len(files)})
	}
	impact := profileImpact{Profile: *p, Tables: tables, FolderKept: true}
	if d.JSONOutput() {
		return printJSON(impact)
	}
	fmt.Printf("Deleting %s would remove:\n", p.Name)
	for _, t := range tables {
		fmt.Printf("  %-32s %d\n", t.Table, t.Rows)
	}
	fmt.Printf("and its vault key in the OS keychain. Its folder %s would be left on disk.\n", p.RootDir)
	return nil
}

// refuseRunning refuses deleting a profile while a workflow of it is running.
func refuseRunning(ctx context.Context, q scopeQuerier, id string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_executions WHERE profile_id = ? AND status IN ('RUNNING', 'QUEUED')`, id).Scan(&n); err != nil {
		return fmt.Errorf("check running workflows: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("the profile has %d running or queued workflow execution(s): stop them first", n)
	}
	return nil
}

// deleteProfile removes everything the profile owns, and then the profile,
// all or nothing. What it owns is read inside the transaction, so a row added
// since the caller looked is not missed. It returns the ids of the profile's
// workflows and what was removed.
func deleteProfile(ctx context.Context, db *sql.DB, id string) ([]string, []profileTable, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	// Rows in child tables that reference these through their own keys are
	// checked at commit, after the cascades have run.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return nil, nil, err
	}
	if err := refuseRunning(ctx, tx, id); err != nil {
		return nil, nil, err
	}
	scope, err := profileScope(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	tables, err := countScope(ctx, tx, scope, id)
	if err != nil {
		return nil, nil, err
	}
	wfIDs, err := queryStrings(ctx, tx, `SELECT id FROM workflows WHERE profile_id = ?`, id)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range scope {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "`+e.Table+`" WHERE `+e.Where, sql.Named("p", id)); err != nil {
			return nil, nil, fmt.Errorf("delete from %s: %w", e.Table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id); err != nil {
		return nil, nil, fmt.Errorf("delete profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'active_profile_id' AND value = ?`, id); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("delete profile: %w", err)
	}
	return wfIDs, tables, nil
}

func (d Deps) newProfileBackupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "The database snapshots taken before deleting a profile",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List the pre-delete snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := d.OpenDB()
			if err != nil {
				return err
			}
			defer db.Close()
			paths, err := db.ProfileDeleteBackups()
			if err != nil {
				return err
			}
			if d.JSONOutput() {
				if paths == nil {
					paths = []string{}
				}
				return printJSON(paths)
			}
			for _, p := range paths {
				fmt.Println(p)
			}
			return nil
		},
	}
	var yes bool
	purge := &cobra.Command{
		Use:   "purge",
		Short: "Delete every pre-delete snapshot",
		Long:  "Delete every pre-delete-profile-*.db snapshot. " + backupNote + " Only the user can run it.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.refuseNotOperator("purges backups of"); err != nil {
				return err
			}
			if !yes {
				return d.InvalidInput("purging deletes the snapshots for good: pass --yes")
			}
			db, err := d.OpenDB()
			if err != nil {
				return err
			}
			defer db.Close()
			paths, err := db.ProfileDeleteBackups()
			if err != nil {
				return err
			}
			for _, p := range paths {
				if err := os.Remove(p); err != nil {
					return fmt.Errorf("remove %s: %w", p, err)
				}
			}
			if d.JSONOutput() {
				return printJSON(map[string]int{"removed": len(paths)})
			}
			fmt.Printf("Removed %d snapshot(s).\n", len(paths))
			return nil
		},
	}
	purge.Flags().BoolVar(&yes, "yes", false, "Confirm")
	cmd.AddCommand(list, purge)
	return cmd
}
