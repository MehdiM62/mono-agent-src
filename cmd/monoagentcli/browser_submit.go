package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// browser submit --hil <id>: perform a form submit a person approved.

func newBrowserSubmitCmd(cfg *globalConfig) *cobra.Command {
	var hilID string
	cmd := &cobra.Command{
		Use:   "submit --hil <id>",
		Short: "Perform a form submit that waited for approval, once it is approved",
		Long: "A form submit an agent tried on an allowed page that is not local waits for approval\n" +
			"as a Human-in-Loop item. Once a person approved it (`hil approve <id>` or the Human in\n" +
			"Loop page), this lets exactly that one submit through on the same tab: the recorded\n" +
			"submit control is clicked again, or the form submitted. One approval allows one\n" +
			"submit. The tab must still be on the approved origin; --tab and --browser are ignored.",
		Example: "  monoagentcli --json browser submit --hil 1a2b3c4d",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hilID == "" {
				return errInvalidInput("--hil <id> is required: the id the held submit printed")
			}
			ctx := cmd.Context()
			db, err := initDB(cfg)
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.Close()
			it, err := browserctl.ClaimSubmitItem(ctx, db.DB, cfg.ProfileID, hilID)
			if err != nil {
				return browserErr(cmd, cfg, err)
			}
			// Until something reached the page the approval is handed back.
			giveBack := func(err error) error {
				_ = browserctl.ReleaseSubmitItem(ctx, db.DB, cfg.ProfileID, it.ID)
				return browserErr(cmd, cfg, err)
			}
			sender, err := browserBridge()
			if err != nil {
				return giveBack(err)
			}
			allow, err := browserctl.LoadAllowlist(db.DB)
			if err != nil {
				return giveBack(err)
			}
			s, err := browserctl.Open(browserctl.Options{
				Browser: it.Instance, Profile: cfg.ProfileID, Tab: it.TabID, Allowlist: allow, Sender: sender,
			})
			if err != nil {
				return giveBack(err)
			}
			if now := browserctl.Origin(s.URL()); now != it.Origin {
				return giveBack(errInvalidInput("tab %d is now on %s, but the approval was for a submit on %s", it.TabID, now, it.Origin))
			}
			err = s.Resubmit(it)
			if errors.Is(err, browserctl.ErrNotSent) {
				return giveBack(err)
			}
			browserctl.Settle()
			_ = browserctl.FinishSubmitItem(ctx, db.DB, cfg.ProfileID, it.ID, err)
			if err != nil {
				return browserErr(cmd, cfg, err)
			}
			url, _ := s.Refresh()
			return browserOut(cmd, cfg, map[string]any{"hil_id": it.ID, "status": "submitted", "tabId": s.TabID(), "url": url}, func() {
				fmt.Fprintf(cmd.OutOrStdout(), "Submitted on %s (approval %s used)\n", it.Origin, it.ID)
			})
		},
	}
	cmd.Flags().StringVar(&hilID, "hil", "", "The approved Human-in-Loop item's id")
	return cmd
}
