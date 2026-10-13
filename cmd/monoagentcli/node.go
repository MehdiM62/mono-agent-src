package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/bot"
	_ "github.com/monoes/mono-agent/internal/bot/gemini"
	_ "github.com/monoes/mono-agent/internal/bot/instagram"
	_ "github.com/monoes/mono-agent/internal/bot/linkedin"
	_ "github.com/monoes/mono-agent/internal/bot/tiktok"
	_ "github.com/monoes/mono-agent/internal/bot/x"
	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/i18n"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/nodes"
	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// reLinkedInActivity matches the numeric activity ID in LinkedIn post URLs.
// e.g. "activity-7123456789" or "activity:7123456789"
var reLinkedInActivity = regexp.MustCompile(`activity[-:](\d+)`)

// needsBrowserSession reports whether a node type drives the browser and so
// needs the extension session provider: every automation action (built-in
// or installed package, all BrowserNode) and the browser.* nodes.
func needsBrowserSession(nodeType string, factory workflow.NodeFactory) bool {
	if strings.HasPrefix(nodeType, "browser.") {
		return true
	}
	_, ok := factory().(*nodes.BrowserNode)
	return ok
}

// nodeTypeToPlatform maps a node type to its connections-registry platform ID.
// The mapping covers node-type prefixes that don't match their platform ID directly.
var nodeTypeToPlatformOverrides = map[string]string{
	"db.postgres": "postgresql",
	"db.mysql":    "mysql",
	"db.mongodb":  "mongodb",
	"db.redis":    "redis",
}

// nodeTypeToPlatform derives the connections-registry platform ID from a node type.
// Examples:
//
//	"service.google_sheets" → "google_sheets"
//	"service.github"        → "github"
//	"comm.slack"            → "slack"
//	"db.postgres"           → "postgresql"
//	"google_sheets"         → "google_sheets"  (legacy unprefixed)
func nodeTypeToPlatform(nodeType string) string {
	if p, ok := nodeTypeToPlatformOverrides[nodeType]; ok {
		return p
	}
	// Strip known category prefixes.
	for _, prefix := range []string{"service.", "comm.", "db."} {
		if strings.HasPrefix(nodeType, prefix) {
			return strings.TrimPrefix(nodeType, prefix)
		}
	}
	// Already a bare platform name (legacy alias).
	return nodeType
}

// resolveCredentialData looks up a connection by ID or platform name, checks for
// token expiry, and returns the credential data map. This mirrors the Wails app's
// getResourceCredentialData function.
func resolveCredentialData(ctx context.Context, store *connections.Store, credentialOrPlatform, profileID string) (map[string]interface{}, error) {
	if store == nil {
		return nil, fmt.Errorf("connections store not available")
	}
	// Try by ID first.
	conn, err := store.Get(ctx, credentialOrPlatform)
	// A connection ID is unambiguous, but still confirm it belongs to the
	// active profile — passing another profile's connection ID (by mistake,
	// or a stale ID from a previous --profile) must not silently succeed
	// against the wrong mailbox/account.
	if err == nil && conn != nil && conn.ProfileID != "" && conn.ProfileID != profileID {
		conn = nil
		err = fmt.Errorf("connection %q belongs to a different profile", credentialOrPlatform)
	}
	if (err != nil || conn == nil) && credentialOrPlatform != "" {
		// Fallback: look up by platform name, scoped to the active profile —
		// unscoped lookup here previously matched whichever profile's
		// connection for this platform happened to be found first,
		// silently operating on the wrong account when multiple profiles
		// have a connection for the same platform.
		conns, lErr := store.ListUsableByPlatform(ctx, credentialOrPlatform, profileID)
		if lErr == nil && len(conns) > 0 {
			for i := range conns {
				if conns[i].Status == "active" {
					conn = &conns[i]
					break
				}
			}
			if conn == nil {
				conn = &conns[0]
			}
		}
	}
	if conn == nil {
		return nil, fmt.Errorf("no connection found for %q — run `monoagentcli connect %s` first", credentialOrPlatform, credentialOrPlatform)
	}

	// Check if OAuth token needs refresh (expires within 60 seconds).
	if expiresStr, _ := conn.Data["expires_at"].(string); expiresStr != "" {
		if expiresAt, err := time.Parse(time.RFC3339, expiresStr); err == nil {
			if time.Now().UTC().After(expiresAt.Add(-60 * time.Second)) {
				// Shares the same silent refresh_token exchange (with the
				// per-profile client-credential lookup and /consumers/
				// audience fallback) as every other refresh call site —
				// GUI, daemon, and `connect refresh` all go through this one
				// implementation now instead of each keeping its own copy.
				if err := store.RefreshToken(ctx, conn); err == nil {
					return conn.Data, nil
				} else {
					fmt.Fprintf(os.Stderr, "  Warning: token refresh failed, using existing token: %v\n", err)
				}
			}
		}
	}

	return conn.Data, nil
}

// extensionListenAddr is the address this process's own extension server
// tries to bind: the env override (extension.ExtensionPortEnv) when set,
// otherwise the shared default port — whose EADDRINUSE fallback to 9323
// (e.g. when Chrome's own CDP holds 9222) lives in extension.Server.
func extensionListenAddr() string {
	if p := strings.TrimSpace(os.Getenv(extension.ExtensionPortEnv)); p != "" {
		return "127.0.0.1:" + p
	}
	return "127.0.0.1:" + extension.DefaultExtensionPort
}

// extensionProbeAddrs lists the HTTP base URLs where an already-running
// monoagent relay may be found: the env-overridden port when set, otherwise
// the default port and its 9323 fallback (mirroring extension.Server's bind
// candidates).
func extensionProbeAddrs() []string {
	if p := strings.TrimSpace(os.Getenv(extension.ExtensionPortEnv)); p != "" {
		return []string{"http://127.0.0.1:" + p}
	}
	return []string{
		"http://127.0.0.1:" + extension.DefaultExtensionPort,
		"http://127.0.0.1:" + extension.FallbackExtensionPort,
	}
}

// waitForRelay polls addr until a monoagent relay answers its health endpoint,
// returning a bridge through it, or nil if none appears within timeout. The
// wait covers the gap between another process binding the port and its HTTP
// server actually serving.
func waitForRelay(addr string, timeout time.Duration) browserpkg.ExtensionBridge {
	deadline := time.Now().Add(timeout)
	for {
		if extension.Probe(addr) {
			return &extension.RemoteBridge{Sender: extension.NewRemoteSender(addr)}
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// setupExtensionBridge returns a browser.ExtensionBridge for talking to the
// Chrome extension. If another local monoagentcli process (typically a
// `monoagentcli extension serve` bridge, or the daemon) already owns the
// extension connection, it relays through that process's server instead of
// starting a second one — starting a second server would just fail to bind
// the fixed extension port and silently leave IsConnected() reporting
// false, since there is no Rod/Chromium fallback to degrade to.
func setupExtensionBridge(logger zerolog.Logger, waitForConnection time.Duration) browserpkg.ExtensionBridge {
	probeAddrs := extensionProbeAddrs()
	for _, addr := range probeAddrs {
		if extension.Probe(addr) {
			fmt.Fprintln(os.Stderr, "  Reusing existing extension connection (shared with another monoagentcli process)")
			return &extension.RemoteBridge{Sender: extension.NewRemoteSender(addr)}
		}
	}

	// Same wiring an `extension serve` bridge gets (see extension_serve.go),
	// so a workflow that has to start its own bridge is not a lesser one
	// than the bridge it would otherwise have relayed through.
	extServer := newExtensionServer(logger)
	errCh := extServer.StartAsync(context.Background())

	// The probe above and the bind below are not atomic: another process can
	// claim the port in between (two runs started together, or the daemon/GUI
	// coming up). Losing that race is normal and recoverable — re-probe and
	// relay through the winner instead of reporting the port as unusable.
	connCh := make(chan struct{})
	go func() {
		_ = extServer.WaitForConnection(waitForConnection)
		close(connCh)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			// Note: with the server-side fallback (9222 → 9323) an errCh
			// delivery now means BOTH candidate ports failed to bind.
			for _, addr := range probeAddrs {
				if remote := waitForRelay(addr, 5*time.Second); remote != nil {
					fmt.Fprintln(os.Stderr, "  Reusing existing extension connection (another process owns the extension port)")
					return remote
				}
			}
			fmt.Fprintf(os.Stderr, "  Extension ports %s are held by processes that are not monoagent relays (%v)\n", strings.Join(probeAddrs, ", "), err)
			fmt.Fprintln(os.Stderr, "  Free them (e.g. a Chrome started with --remote-debugging-port=9222) or set MONOAGENT_EXTENSION_PORT, and retry.")
		}
	case <-connCh:
	}

	if extServer.IsConnected() {
		fmt.Fprintln(os.Stderr, "  ✓ Chrome extension connected -- using your browser")
	} else {
		fmt.Fprintln(os.Stderr, "  Chrome extension not connected -- no browser will be launched as a fallback")
	}
	return &extension.ServerBridge{Server: extServer}
}

// cliBotRegistry wraps bot.PlatformRegistry to satisfy nodes.BotRegistry.
type cliBotRegistry struct{}

func (r *cliBotRegistry) GetAdapter(platform string) (action.BotAdapter, bool) {
	factory, ok := bot.PlatformRegistry[strings.ToUpper(platform)]
	if !ok {
		return nil, false
	}
	adapter := factory()
	if ba, ok := adapter.(action.BotAdapter); ok {
		return ba, true
	}
	return nil, false
}

// buildNodeRegistry creates a registry with all built-in node types registered.
// If db is non-nil, AI nodes are also registered (they need an AIStore backed by the DB).
func buildNodeRegistry(_ bool, db *sql.DB) *workflow.NodeTypeRegistry {
	return noderegistry.Build(db)
}

// newNodeCmd returns the `node` command with subcommands.
func newNodeCmd(cfg *globalConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: i18n.T("node.short"),
	}
	cmd.AddCommand(
		newNodeListCmd(cfg),
		newNodeRunCmd(cfg),
		newNodeSchemaCmd(cfg),
		newNodePaletteCmd(cfg),
		newNodeCurlCmd(cfg),
	)
	return cmd
}

// newNodeSchemaCmd prints a node type's embedded JSON schema (pretty-printed).
func newNodeSchemaCmd(cfg *globalConfig) *cobra.Command {
	return &cobra.Command{
		Use:     "schema <type>",
		Short:   i18n.T("node.schema.short"),
		Example: i18n.T("node.schema.example"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, ok := workflow.ReadEmbeddedSchema(args[0])
			if !ok {
				return errNotFound("no schema for node type %q — run `monoagentcli node list` to see available types", args[0])
			}
			var buf bytes.Buffer
			if err := json.Indent(&buf, data, "", "  "); err != nil {
				return fmt.Errorf("format schema for %s: %w", args[0], err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), buf.String())
			return nil
		},
	}
}

// nodeListEntry is one element of `node list --json`.
type nodeListEntry struct {
	Type     string `json:"type"`
	Category string `json:"category"`
	Title    string `json:"title"`
}

// nodeTypeCategory returns the category portion of a node type — the prefix
// before the first dot (core, comm, db, http, …). Legacy unprefixed types
// fall back to the heuristic nodeCategory mapping.
func nodeTypeCategory(t string) string {
	if i := strings.Index(t, "."); i > 0 {
		return t[:i]
	}
	return nodeCategory(t)
}

// triggerNodeTypes are the trigger types the engine handles itself: they are
// never in the node registry (execution.go passes them through), so `node
// list` adds them to the registered types.
var triggerNodeTypes = []string{"trigger.manual", "trigger.org", "trigger.schedule", "trigger.webhook"}

// newNodeListCmd lists all registered node types and the engine's triggers.
func newNodeListCmd(cfg *globalConfig) *cobra.Command {
	var filter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all available node types",
		Example: `  monoagentcli node list
  monoagentcli node list --filter comm
  monoagentcli node list --filter http`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open DB on best-effort basis so AI nodes appear in the list.
			var rawDB *sql.DB
			if db, err := initDB(cfg); err == nil {
				rawDB = db.DB
				defer db.Close()
			}
			registry := buildNodeRegistry(cfg.Verbose, rawDB)
			types := registry.Types()
			for _, t := range triggerNodeTypes {
				if !registry.Has(t) {
					types = append(types, t)
				}
			}
			sort.Strings(types)

			if filter != "" {
				var filtered []string
				for _, t := range types {
					if strings.Contains(t, filter) {
						filtered = append(filtered, t)
					}
				}
				types = filtered
			}

			if cfg.JSONOutput {
				entries := make([]nodeListEntry, 0, len(types))
				for _, t := range types {
					entries = append(entries, nodeListEntry{
						Type:     t,
						Category: nodeTypeCategory(t),
						Title:    workflow.SchemaTitle(t),
					})
				}
				return json.NewEncoder(os.Stdout).Encode(entries)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "TYPE\tCATEGORY")
			fmt.Fprintln(w, "────\t────────")
			for _, t := range types {
				fmt.Fprintf(w, "%s\t%s\n", t, nodeTypeCategory(t))
			}
			w.Flush()
			fmt.Printf("\n%d node types\n", len(types))
			return nil
		},
	}

	cmd.Flags().StringVar(&filter, "filter", "", "Filter node types by substring")
	return cmd
}

// nodeCategory infers a display category from a node type string.
func nodeCategory(t string) string {
	switch {
	case strings.HasPrefix(t, "trigger."):
		return "trigger"
	case strings.HasPrefix(t, "control."), t == "if" || t == "switch" || t == "merge" || t == "set" || t == "code" || t == "filter" || t == "sort" || t == "limit" || t == "aggregate" || t == "wait":
		return "control"
	case strings.HasPrefix(t, "http."):
		return "http"
	case strings.HasPrefix(t, "system."):
		return "system"
	case strings.HasPrefix(t, "comm."):
		return "communication"
	case strings.HasPrefix(t, "ai."):
		return "ai"
	case strings.HasPrefix(t, "instagram."), strings.HasPrefix(t, "linkedin."), strings.HasPrefix(t, "x."), strings.HasPrefix(t, "tiktok."), strings.HasPrefix(t, "hackernews."), strings.HasPrefix(t, "producthunt."):
		return "browser/social"
	case strings.HasPrefix(t, "people."):
		return "people"
	case strings.HasPrefix(t, "app."):
		return "app"
	case t == "mysql" || t == "postgres" || t == "mongodb" || t == "redis":
		return "database"
	case t == "github" || t == "notion" || t == "airtable" || t == "jira" || t == "linear" || t == "asana" || t == "stripe" || t == "shopify" || t == "salesforce" || t == "hubspot" || t == "google_sheets" || t == "gmail" || t == "google_drive":
		return "service"
	case t == "datetime" || t == "crypto" || t == "html" || t == "xml" || t == "markdown" || t == "spreadsheet" || t == "compression" || t == "write_binary_file":
		return "data"
	default:
		return "other"
	}
}
