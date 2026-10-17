package extensioncli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// Which browser each profile runs in. A browser is one browser profile with
// the MonoAgent Bridge extension; the binding is stored in that extension,
// and these commands read it through the bridge or ask the extension to
// change it.

// browserRow is one connected browser as `extension browsers --json`
// prints it. The desktop app reads these keys.
type browserRow struct {
	Instance    string `json:"instance"`
	Label       string `json:"label"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Legacy      bool   `json:"legacy"`
	Conflict    bool   `json:"conflict"`
	Version     string `json:"version"`
	ConnectedAt string `json:"connected_at"`
}

type browsersReport struct {
	Running  bool         `json:"running"`
	Hint     string       `json:"hint,omitempty"`
	Browsers []browserRow `json:"browsers"`
}

const noBridgeHint = "No bridge is running. Start one with `monoagentcli extension serve`."

func newExtensionBrowsersCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "browsers",
		Short: "List the browsers attached to the bridge and the profile each one runs",
		Long: "Every browser profile with the MonoAgent Bridge extension is its own browser here.\n" +
			"A browser bound to a profile runs that profile's browser actions, logged in as that\n" +
			"browser's accounts. An unbound browser is the default for profiles with no browser\n" +
			"of their own. Bind one with `extension bind` or in the extension's side panel.",
		Example: "  monoagentcli extension browsers\n  monoagentcli extension browsers --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExtensionBrowsers(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
}

func newExtensionBindCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "bind <browser> <profile>",
		Short: "Run a profile's browser actions in one browser",
		Long: "<browser> is an instance id (or its first 4+ characters) or a label from\n" +
			"`extension browsers`; <profile> is a profile id or name. The browser keeps the\n" +
			"binding across restarts; it is stored in that browser's extension.",
		Example: "  monoagentcli extension bind \"Edge Work\" Work\n  monoagentcli extension bind 3f2a Personal",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := cfg.OpenDB()
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			profileID, err := cfg.ResolveProfileID(db.DB, args[1])
			db.Close()
			if err != nil {
				return err
			}
			return runExtensionBind(cmd.Context(), cfg, cmd.OutOrStdout(), args[0], profileID)
		},
	}
}

func newExtensionUnbindCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "unbind <browser>",
		Short:   "Make a browser a default browser again (for profiles with none of their own)",
		Example: "  monoagentcli extension unbind \"Edge Work\"",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExtensionBind(cmd.Context(), cfg, cmd.OutOrStdout(), args[0], "")
		},
	}
}

func runExtensionBrowsers(ctx context.Context, cfg *Deps, out io.Writer) error {
	report := browsersReport{Browsers: []browserRow{}}
	_, base, ok := findRunningBridge()
	if !ok {
		report.Hint = noBridgeHint
		return printBrowsers(out, cfg.JSONOutput(), report)
	}
	report.Running = true
	infos, err := extension.NewRemoteSender(base).Browsers()
	switch {
	case errors.Is(err, extension.ErrBridgeTooOld):
		report.Hint = err.Error()
	case err != nil:
		return clicore.AuthConnection("listing browsers: %v", err)
	default:
		report.Browsers = browserRows(infos, profileNames(ctx, cfg))
	}
	return printBrowsers(out, cfg.JSONOutput(), report)
}

func runExtensionBind(ctx context.Context, cfg *Deps, out io.Writer, query, profileID string) error {
	_, base, ok := findRunningBridge()
	if !ok {
		return clicore.NotFound("%s", noBridgeHint)
	}
	sender := extension.NewRemoteSender(base)
	infos, err := sender.Browsers()
	if err != nil {
		return clicore.AuthConnection("listing browsers: %v", err)
	}
	target, err := matchBrowser(infos, query)
	if err != nil {
		return err
	}
	if target.Legacy {
		return clicore.InvalidInput("that browser's extension is too old to bind — reload it from the browser's extensions page (it needs version 1.5.0 or later)")
	}
	if err := sender.SetBinding(target.Instance, profileID, ""); err != nil {
		return fmt.Errorf("binding %s: %w (if the extension is older than 1.5.0, reload it)", target.Instance, err)
	}
	infos, err = sender.Browsers()
	if err != nil {
		return clicore.AuthConnection("listing browsers: %v", err)
	}
	for _, row := range browserRows(infos, profileNames(ctx, cfg)) {
		if row.Instance == target.Instance {
			if cfg.JSONOutput() {
				return json.NewEncoder(out).Encode(row)
			}
			printBrowserTable(out, []browserRow{row})
			return nil
		}
	}
	return clicore.NotFound("the browser disconnected right after binding — check `monoagentcli extension browsers`")
}

// matchBrowser finds one browser by instance id, id prefix (4+ chars) or
// label (case-insensitive).
func matchBrowser(infos []extension.ConnInfo, query string) (extension.ConnInfo, error) {
	q := strings.TrimSpace(query)
	var hits []extension.ConnInfo
	for _, b := range infos {
		if b.Instance == q {
			return b, nil
		}
	}
	for _, b := range infos {
		if (len(q) >= 4 && strings.HasPrefix(b.Instance, q)) || strings.EqualFold(b.Label, q) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return extension.ConnInfo{}, clicore.NotFound("no connected browser matches %q — see `monoagentcli extension browsers`", q)
	default:
		return extension.ConnInfo{}, clicore.InvalidInput("%q matches %d browsers — use more of the id", q, len(hits))
	}
}

func browserRows(infos []extension.ConnInfo, names map[string]string) []browserRow {
	rows := make([]browserRow, 0, len(infos))
	for _, b := range infos {
		row := browserRow{
			Instance: b.Instance, Label: b.Label, ProfileID: b.Profile, ProfileName: names[b.Profile],
			Legacy: b.Legacy, Conflict: b.Conflict, Version: b.Version,
		}
		if !b.ConnectedAt.IsZero() {
			row.ConnectedAt = b.ConnectedAt.Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}

// profileNames maps profile ids to names for display; it is best effort.
func profileNames(ctx context.Context, cfg *Deps) map[string]string {
	names := map[string]string{}
	db, err := cfg.OpenDB()
	if err != nil {
		return names
	}
	defer db.Close()
	list, _ := profiledir.List(ctx, db.DB)
	for _, p := range list {
		names[p.ID] = p.Name
	}
	return names
}

func printBrowsers(out io.Writer, asJSON bool, report browsersReport) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if report.Hint != "" {
		fmt.Fprintln(out, report.Hint)
	}
	if report.Running && len(report.Browsers) == 0 && report.Hint == "" {
		fmt.Fprintln(out, "No browser is attached. Open a browser that has the MonoAgent Bridge extension.")
		return nil
	}
	if len(report.Browsers) > 0 {
		printBrowserTable(out, report.Browsers)
	}
	return nil
}

func printBrowserTable(out io.Writer, rows []browserRow) {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "BROWSER\tID\tPROFILE\tNOTE")
	for _, r := range rows {
		label := r.Label
		if label == "" {
			label = "(unnamed)"
		}
		id := r.Instance
		if len(id) > 8 {
			id = id[:8]
		}
		profile := "any profile"
		if r.ProfileID != "" {
			profile = r.ProfileName
			if profile == "" {
				profile = r.ProfileID + " (unknown profile)"
			}
		}
		note := ""
		switch {
		case r.Legacy:
			note = "extension too old to bind — reload it"
		case r.Conflict:
			note = "another browser is bound to this profile; the newer one is used"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", label, id, profile, note)
	}
	_ = tw.Flush()
}

// bindingBridge is the part of the bridge UnbindProfile uses.
type bindingBridge interface {
	Browsers() ([]extension.ConnInfo, error)
	SetBinding(instance, profile, label string) error
}

// UnbindProfile makes every connected browser bound to profileID unbound
// again, for deleting the profile, and returns how many it changed. No bridge
// running is nothing to do. A browser that fails to unbind does not stop the
// others: every failure is reported together. A browser that is not connected
// keeps its binding in its extension until it connects and is unbound by hand.
func UnbindProfile(ctx context.Context, profileID string) (int, error) {
	_, base, ok := findRunningBridgeCtx(ctx)
	if !ok {
		return 0, nil
	}
	return unbindProfile(extension.NewRemoteSender(base), profileID)
}

func unbindProfile(b bindingBridge, profileID string) (int, error) {
	infos, err := b.Browsers()
	if err != nil {
		return 0, fmt.Errorf("listing browsers: %w", err)
	}
	n := 0
	var errs []error
	for _, info := range infos {
		if info.Profile != profileID || info.Legacy {
			continue
		}
		if err := b.SetBinding(info.Instance, "", ""); err != nil {
			errs = append(errs, fmt.Errorf("unbinding %s: %w", info.Instance, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}
