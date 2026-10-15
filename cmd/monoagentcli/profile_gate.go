package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"

	"github.com/monoes/mono-agent/internal/cli/profilegate"
)

// The profile gate (internal/cli/profilegate): a command that acts on a
// profile must be told which one with --profile. It runs in warn mode for one
// release: the warning goes to stderr and the command runs as before.
// profileGateMode becomes ModeEnforce when the gate is turned on.
var profileGateMode = profilegate.ModeWarn

// installProfileGate makes every command run the gate before it runs. The gate
// reads whether --profile was given from the parsed flags, so it must come
// before anything (initDB) resolves cfg.ProfileID. A command with a
// PersistentPreRun of its own (doctor, setup) replaces the root's, so each of
// those is wrapped to run the gate first.
func installProfileGate(root *cobra.Command, cfg *globalConfig) {
	gate := func(cmd *cobra.Command) {
		list := func() ([]profilegate.ProfileRef, error) { return listProfilesReadOnly(cfg.DBPath) }
		warning, err := profilegate.Check(cmd, cmd.Flags().Changed("profile"), profileGateMode, list)
		if err == nil && warning != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
	}
	var wrap func(c *cobra.Command)
	wrap = func(c *cobra.Command) {
		if run := c.PersistentPreRun; run != nil {
			c.PersistentPreRun = func(cmd *cobra.Command, args []string) {
				gate(cmd)
				run(cmd, args)
			}
		}
		for _, sub := range c.Commands() {
			wrap(sub)
		}
	}
	wrap(root)
}

// readOnlyURI is the SQLite URI that opens the file at path read-only. The path
// is escaped (a ?, # or % in a folder name is part of the name), and a Windows
// drive path gets the leading slash a file URI needs.
func readOnlyURI(path string) string {
	p := filepath.ToSlash(path)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	return u.String()
}

// listProfilesReadOnly names the profiles of the database at dbPath for the
// gate's message. Best effort: it never creates or migrates a database, and
// any failure means no list.
func listProfilesReadOnly(dbPath string) ([]profilegate.ProfileRef, error) {
	path := expandPath(dbPath)
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, name FROM profiles ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []profilegate.ProfileRef
	for rows.Next() {
		var r profilegate.ProfileRef
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
