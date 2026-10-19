package hil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// A form submit an agent tried on a page that is not local (internal/browserctl:
// a HIL item of workflow browser.submit) is the operator's to decide, like a send:
// an agent is refused, and an approval is stamped so `browser submit --hil` can
// tell it came from here and not from a path an agent reaches. So is a site an
// agent asked to use (browser.site, level ask): approving it adds an allow rule.

// decideIfBrowserSubmit decides id when it is a browser submit or site item; done is
// false for any other item.
func (d Deps) decideIfBrowserSubmit(cmd *cobra.Command, id string, approve bool) (done bool, err error) {
	db, err := d.OpenDB()
	if err != nil {
		return false, nil
	}
	defer db.Close()
	var nc string
	if err := db.DB.QueryRowContext(cmd.Context(), `SELECT node_config FROM hil_pending WHERE id = ? AND profile_id = ?`,
		id, d.ProfileID()).Scan(&nc); err != nil {
		return false, nil
	}
	var itemID, status string
	switch {
	case browserctl.IsSubmitItem(nc):
		if err := clicore.RequireOperator(d.RequireOperator, "approve or reject a browser form submit"); err != nil {
			return true, err
		}
		it, err := browserctl.DecideSubmitItem(cmd.Context(), db.DB, d.ProfileID(), id, approve)
		if err != nil {
			return true, d.decisionError(id, err)
		}
		itemID, status = it.ID, it.Status
	case browserctl.IsSiteItem(nc):
		// An agent asked to use a site (level ask): approving adds an allow rule.
		if err := clicore.RequireOperator(d.RequireOperator, "let an agent use a site"); err != nil {
			return true, err
		}
		it, err := browserctl.DecideSiteItem(cmd.Context(), db.DB, d.ProfileID(), id, approve)
		if err != nil {
			return true, d.decisionError(id, err)
		}
		itemID, status = it.ID, it.Status
	default:
		return false, nil
	}
	// No workflow run waits on it, so printDecision's daemon hint does not apply.
	if d.JSONOutput() {
		return true, json.NewEncoder(os.Stdout).Encode(map[string]string{"id": itemID, "status": status})
	}
	verb := "Rejected"
	if approve {
		verb = "Approved"
	}
	fmt.Printf("%s HIL item %s\n", verb, itemID)
	return true, nil
}

// decisionError reports an item already decided as not found, as `hil
// approve` does for any item, and a site a block covers as invalid input.
func (d Deps) decisionError(id string, err error) error {
	var se *browserctl.SubmitStateError
	if errors.As(err, &se) {
		return d.NotFound("HIL item %q not found or already resolved", id)
	}
	var blocked *browserctl.SiteBlockedDecisionError
	if errors.As(err, &blocked) {
		return d.InvalidInput("%s", blocked.Error())
	}
	return err
}
