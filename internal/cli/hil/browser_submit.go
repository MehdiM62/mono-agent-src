package hil

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/extension"
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
	if claim, ok := extension.IsPairCard(nc); ok {
		return d.decidePairCard(cmd, db.DB, id, claim, approve)
	}
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

// pairCardCode is the confirmation code the owner typed for a pairing card:
// in --data, or in the card's editable "code" field.
func pairCardCode(cmd *cobra.Command, db *sql.DB, cardID string) string {
	var doc struct {
		Code string `json:"code"`
	}
	if f := cmd.Flags().Lookup("data"); f != nil {
		_ = json.Unmarshal([]byte(f.Value.String()), &doc)
	}
	if doc.Code == "" {
		var raw string
		if db.QueryRowContext(cmd.Context(), `SELECT editable_data FROM hil_pending WHERE id = ?`, cardID).Scan(&raw) == nil {
			_ = json.Unmarshal([]byte(raw), &doc)
		}
	}
	return doc.Code
}

// decidePairCard decides a browser's request to pair (a card the bridge filed
// for extension.autoPair=ask). It is the owner's: an agent is refused. Only
// this path and `extension approve` ever approve a claim, so an approval made
// on the row by any other route (the MCP hil tools, the HTTP API) changes
// nothing about the claim.
func (d Deps) decidePairCard(cmd *cobra.Command, db *sql.DB, cardID, claim string, approve bool) (bool, error) {
	if err := clicore.RequireOperator(d.RequireOperator, "approve or deny a browser's request to pair"); err != nil {
		return true, err
	}
	code := ""
	if approve {
		code = pairCardCode(cmd, db, cardID)
	}
	c, err := extension.DecidePendingClaim(claim, approve, code)
	status := "rejected"
	if approve {
		status = "approved"
	}
	if errors.Is(err, extension.ErrBadCode) {
		// Not decided: the card stays for the owner to try again with the code their browser shows.
		return true, d.InvalidInput("%v (type the code in the card's code field, or pass --data '{\"code\":\"ABC-123\"}')", err)
	}
	if err != nil {
		// Gone, expired or already decided: the card has nothing left to decide.
		_ = extension.SettlePairCards(cmd.Context(), db, claim, "expired")
		return true, d.NotFound("HIL item %q not found or already resolved", cardID)
	}
	_ = extension.SettlePairCards(cmd.Context(), db, c.ID, status)
	if d.JSONOutput() {
		return true, json.NewEncoder(os.Stdout).Encode(map[string]string{"id": cardID, "status": status})
	}
	verb := "Denied"
	if approve {
		verb = "Approved"
	}
	fmt.Printf("%s HIL item %s\n", verb, cardID)
	return true, nil
}
