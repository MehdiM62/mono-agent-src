package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/monoes/mono-agent/internal/applications"
	"github.com/monoes/mono-agent/internal/profiledir"

	"github.com/spf13/cobra"
)

// maxFieldsFile bounds a --fields-json file.
const maxFieldsFile = 1 << 20

func (d Deps) writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// mapStoreErr turns the store's sentinel errors into the CLI's exit-code errors.
func (d Deps) mapStoreErr(id string, err error) error {
	switch {
	case errors.Is(err, applications.ErrNotFound):
		return d.NotFound("application %q not found", id)
	case errors.Is(err, applications.ErrInvalidInput):
		return d.InvalidInput("%v", err)
	}
	return err
}

func (d Deps) newLinkCmd() *cobra.Command {
	link := &cobra.Command{Use: "link", Short: "Link an application to a person, message, task, document or capture"}
	var kind, ref string
	add := func(use, short string, run func(cmd *cobra.Command, store *applications.Store, id string) error) *cobra.Command {
		c := &cobra.Command{
			Use: use, Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if kind != "" && !applications.ValidLinkKind(applications.LinkKind(kind)) {
					return d.InvalidInput("--kind must be person, message, task, document or capture, got %q", kind)
				}
				db, err := d.OpenDB()
				if err != nil {
					return fmt.Errorf("initializing database: %w", err)
				}
				defer db.DB.Close()
				return run(cmd, applications.NewStore(db.DB), args[0])
			},
		}
		c.Flags().StringVar(&kind, "kind", "", "person, message, task, document or capture")
		c.Flags().StringVar(&ref, "ref", "", "id of the thing linked")
		return c
	}
	addCmd := add("add <id>", "Link something to an application", func(cmd *cobra.Command, s *applications.Store, id string) error {
		if err := s.AddLink(cmd.Context(), d.ProfileID(), id, applications.LinkKind(kind), ref); err != nil {
			return d.mapStoreErr(id, err)
		}
		return d.writeJSON(cmd, map[string]string{"id": id, "kind": kind, "refId": ref, "action": "add"})
	})
	rmCmd := add("remove <id>", "Remove a link", func(cmd *cobra.Command, s *applications.Store, id string) error {
		removed, err := s.RemoveLink(cmd.Context(), d.ProfileID(), id, applications.LinkKind(kind), ref)
		if err != nil {
			return d.mapStoreErr(id, err)
		}
		return d.writeJSON(cmd, map[string]any{"id": id, "kind": kind, "refId": ref, "action": "remove", "removed": removed})
	})
	listCmd := add("list <id>", "List an application's links (--kind narrows)", func(cmd *cobra.Command, s *applications.Store, id string) error {
		links, err := s.ListLinks(cmd.Context(), d.ProfileID(), id, applications.LinkKind(kind))
		if err != nil {
			return d.mapStoreErr(id, err)
		}
		return d.writeJSON(cmd, links)
	})
	for _, c := range []*cobra.Command{addCmd, rmCmd} {
		c.MarkFlagRequired("kind")
		c.MarkFlagRequired("ref")
	}
	link.AddCommand(addCmd, rmCmd, listCmd)
	return link
}

func (d Deps) newFillCmd() *cobra.Command {
	fill := &cobra.Command{Use: "fill", Short: "Record and show the last form fill of an application"}

	var status, screenshot, note, fieldsFile string
	record := &cobra.Command{
		Use:     "record <id>",
		Short:   "Record the state of a form fill (replaces the previous record)",
		Args:    cobra.ExactArgs(1),
		Example: `  monoagentcli --profile work application fill record 1c2e... --status filled --screenshot ~/work/shot.png --fields-json fields.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			f := applications.Fill{Status: applications.FillStatus(status), Note: note}
			if !applications.ValidFillStatus(f.Status) {
				return d.InvalidInput("--status must be filling, filled, needs_you or submitted, got %q", status)
			}
			if len(note) > applications.MaxNoteLen {
				return d.InvalidInput("--note is over %d bytes", applications.MaxNoteLen)
			}
			if fieldsFile != "" {
				fields, err := readFields(fieldsFile)
				if err != nil {
					return d.InvalidInput("%v", err)
				}
				f.Fields = fields
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()
			if screenshot != "" {
				path, err := insideProfileFolder(profiledir.Root(db.DB, d.ProfileID()), screenshot)
				if err != nil {
					return d.InvalidInput("--screenshot %v", err)
				}
				f.Screenshot = path
			}
			if err := applications.NewStore(db.DB).RecordFill(cmd.Context(), d.ProfileID(), id, f); err != nil {
				return d.mapStoreErr(id, err)
			}
			return d.writeJSON(cmd, map[string]any{"id": id, "status": status, "fields": len(f.Fields)})
		},
	}
	record.Flags().StringVar(&status, "status", "", "filling, filled, needs_you or submitted (required)")
	record.Flags().StringVar(&screenshot, "screenshot", "", "screenshot file inside the profile folder")
	record.Flags().StringVar(&note, "note", "", "short note, e.g. what needs the human")
	record.Flags().StringVar(&fieldsFile, "fields-json", "", "JSON file: [{label,value,source,generated}]")
	record.MarkFlagRequired("status")

	show := &cobra.Command{
		Use: "show <id>", Short: "Show the last recorded form fill", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()
			f, err := applications.NewStore(db.DB).GetFill(cmd.Context(), d.ProfileID(), args[0])
			if err != nil {
				return d.mapStoreErr(args[0], err)
			}
			return d.writeJSON(cmd, f)
		},
	}
	fill.AddCommand(record, show, d.newFillStartCmd(), d.newFillSyncCmd())
	return fill
}

// readFields reads and size-checks a --fields-json file.
func readFields(path string) ([]applications.FillField, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("--fields-json: %v", err)
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("--fields-json: not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxFieldsFile+1))
	if err != nil {
		return nil, fmt.Errorf("--fields-json: %v", err)
	}
	if len(raw) > maxFieldsFile {
		return nil, fmt.Errorf("--fields-json: file is over %d bytes", maxFieldsFile)
	}
	var fields []applications.FillField
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("--fields-json: %v", err)
	}
	return fields, nil
}

// insideProfileFolder resolves path (symlinks included) and returns it only if it is an
// existing regular file inside root.
func insideProfileFolder(root, path string) (string, error) {
	if root == "" {
		return "", errors.New("has no profile folder to check against")
	}
	if strings.ContainsRune(path, 0) {
		return "", errors.New("is not a valid path")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("must be inside the profile folder (the folder does not exist)")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("cannot be read: %v", err)
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("must be inside the profile folder")
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return "", errors.New("must be a regular file")
	}
	return real, nil
}
