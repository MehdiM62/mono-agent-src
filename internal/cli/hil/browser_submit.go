package hil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// A form submit an agent tried on a page that is not local (internal/browserctl:
// a HIL item of workflow browser.submit) is the operator's to decide, like a send:
// an agent is refused, and an approval is stamped so `browser submit --hil` can
// tell it came from here and not from a path an agent reaches.

// decideIfBrowserSubmit decides id when it is a browser submit item; done is false
// for any other item.
func (d Deps) decideIfBrowserSubmit(cmd *cobra.Command, id string, approve bool) (done bool, err error) {
	db, err := d.OpenDB()
	if err != nil {
		return false, nil
	}
	defer db.Close()
	var nc string
	if err := db.DB.QueryRowContext(cmd.Context(), `SELECT node_config FROM hil_pending WHERE id = ? AND profile_id = ?`,
		id, d.ProfileID()).Scan(&nc); err != nil || !browserctl.IsSubmitItem(nc) {
		return false, nil
	}
	if err := d.RequireOperator("approve or reject a browser form submit"); err != nil {
		return true, err
	}
	it, err := browserctl.DecideSubmitItem(cmd.Context(), db.DB, d.ProfileID(), id, approve)
	var se *browserctl.SubmitStateError
	if errors.As(err, &se) {
		return true, d.NotFound("HIL item %q not found or already resolved", id)
	}
	if err != nil {
		return true, err
	}
	// No workflow run waits on it, so printDecision's daemon hint does not apply.
	if d.JSONOutput() {
		return true, json.NewEncoder(os.Stdout).Encode(map[string]string{"id": it.ID, "status": it.Status})
	}
	verb := "Rejected"
	if approve {
		verb = "Approved"
	}
	fmt.Printf("%s HIL item %s\n", verb, it.ID)
	return true, nil
}
