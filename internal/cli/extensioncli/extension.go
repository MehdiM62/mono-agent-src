package extensioncli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/extension"
)

// newExtensionCmd groups the Chrome extension bridge (the loopback
// WebSocket at ~9222/monoagent): running it (`serve`), inspecting it
// (`status`), and pairing with it (`pair`/`reset`). The bridge is
// otherwise only reachable by same-machine callers (loopback bind + Origin
// check), which is not an identity check — any local process could
// impersonate the extension. Pairing gives it one: the server and the
// extension both authenticate against the same shared secret in
// ~/.monoagent/extension.token.
func newExtensionCmd(cfg *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use: "extension",
		// The product calls itself "MonoAgent Bridge" everywhere a user
		// sees it — the extension's name, the pairing page, `extension
		// serve`'s own first line — so someone reading that output and
		// typing `monoagentcli bridge status` deserves an answer rather
		// than "unknown command". `extension` stays canonical: it is what
		// `extension pair` already shipped as, and what the popup prints.
		Aliases: []string{"bridge"},
		Short:   "Run, inspect and pair the MonoAgent Bridge Chrome extension channel",
	}
	cmd.AddCommand(
		newExtensionServeCmd(cfg),
		newExtensionStatusCmd(cfg),
		newExtensionPairCmd(),
		newExtensionResetCmd(),
		newExtensionBrowsersCmd(cfg),
		newExtensionBindCmd(cfg),
		newExtensionUnbindCmd(cfg),
		newExtensionInstallCmd(cfg),
	)
	return cmd
}

func newExtensionPairCmd() *cobra.Command {
	var open bool
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Print the pairing token to paste into the extension side panel (--open pairs it in one click)",
		Long: "Prints the extension bridge's pairing token. Paste this into the MonoAgent\n" +
			"Bridge extension's side panel (\"Pairing token\" field, under Connection settings) to authenticate it — an\n" +
			"unpaired extension cannot connect, and no other local process can\n" +
			"impersonate it without this token.\n" +
			"\n" +
			"The token is stable across restarts once generated; run `extension reset`\n" +
			"first if you need a fresh one (for example, after the token may have\n" +
			"leaked).\n" +
			"\n" +
			"--open asks the running bridge for a one-time pairing page and opens it in\n" +
			"your browser (BROWSER is honored): the extension reads it and pairs itself,\n" +
			"nothing to paste. The page's code works once, for two minutes.",
		Example: "  monoagentcli extension pair --open\n  monoagentcli extension pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			if open {
				return openPairingPage(cmd.OutOrStdout())
			}
			token, err := extension.CurrentToken()
			if err != nil {
				return fmt.Errorf("read extension token: %w", err)
			}
			fmt.Println(token)
			return nil
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "open the running bridge's one-time pairing page in your browser instead of printing the token")
	return cmd
}

// openPairingPage is `extension pair --open`. Wherever the page cannot be
// opened it falls back to the token and the paste instructions, so the
// command always leaves the user with a way to pair.
func openPairingPage(out io.Writer) error {
	_, base, ok := findRunningBridge()
	if !ok {
		fmt.Fprintln(out, "No bridge is running, so there is no pairing page to open. Start one with `monoagentcli extension serve` and run this again, or paste this token into the extension side panel (Connection settings → Pairing token):")
		return printPairingToken(out)
	}
	url, err := extension.RequestPairingURL(base)
	switch {
	case err == nil:
	case errors.Is(err, extension.ErrPairingTokenRefused):
		return fmt.Errorf("%s", extension.PairingMismatchHint)
	case errors.Is(err, extension.ErrPairingPageUnsupported):
		fmt.Fprintln(out, "The running bridge is older and cannot open a pairing page. Paste this token into the extension side panel (Connection settings → Pairing token):")
		return printPairingToken(out)
	default:
		fmt.Fprintf(out, "Could not get a pairing page from the bridge (%v). Paste this token into the extension side panel (Connection settings → Pairing token):\n", err)
		return printPairingToken(out)
	}
	if err := openInBrowser(url); err != nil {
		fmt.Fprintf(out, "Could not open a browser (%v). Open this page in the browser with the extension (it works once, for two minutes):\n  %s\n", err, url)
		return nil
	}
	fmt.Fprintln(out, "Opened the pairing page in your browser. The extension pairs itself; the page says when it is done.")
	fmt.Fprintf(out, "If it opened in a browser without the extension, open this there instead (it works once, for two minutes):\n  %s\n", url)
	return nil
}

func printPairingToken(out io.Writer) error {
	token, err := extension.CurrentToken()
	if err != nil {
		return fmt.Errorf("read extension token: %w", err)
	}
	fmt.Fprintln(out, token)
	return nil
}

// openInBrowser opens url with $BROWSER when it is set (its first entry, as
// in the colon-separated convention), else the system default browser.
var openInBrowser = func(url string) error {
	if b := strings.TrimSpace(strings.Split(os.Getenv("BROWSER"), string(os.PathListSeparator))[0]); b != "" {
		return exec.Command(b, url).Start()
	}
	return clicore.OpenURLInBrowser(url)
}

func newExtensionResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Revoke the current pairing token, forcing the extension to re-pair",
		Long: "Deletes the extension bridge's pairing token. The currently-connected\n" +
			"extension (if any) is not immediately disconnected, but it can no longer\n" +
			"reconnect or be relayed through until you run `extension pair` again and\n" +
			"paste the new token into the extension side panel.\n" +
			"\n" +
			"Use this if you suspect the token has leaked, or as a general \"security\n" +
			"reset\" before handing this machine to someone else.",
		Example: "  monoagentcli extension reset",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := extension.ResetToken(); err != nil {
				return fmt.Errorf("reset extension token: %w", err)
			}
			fmt.Println("Extension token revoked. Run `monoagentcli extension pair` to generate a new one.")
			return nil
		},
	}
}
