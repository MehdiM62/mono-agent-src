package browserctl

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monoes/mono-agent/internal/hilsend"
)

// Site items (plan C8): at level ask, an agent's first visit to a site with
// no rule raises a HIL item asking the user to allow it, instead of a
// refusal. Approving it (operator only, `hil approve` or the HIL page)
// adds an allow rule; rejecting adds nothing. A pending item for the same
// origin and kind is reused. A row's columns:
//
//	execution_id  "browser:site:<id>" (no workflow waits on it)
//	workflow_id   "browser.site"; node_id the same; node_name "Use <host>"
//	readonly_data {origin,url,kind,requested_by}
//	node_config   {kind:"browser.site", agentKind, origin, decidedAt}

// SiteKind marks a site item: its workflow_id, node_id and node_config kind.
const SiteKind = "browser.site"

// SiteApprovalPendingError is a site waiting for the user's yes (HIL item
// HILID). The CLI reports it as site_needs_approval with exit code 3.
type SiteApprovalPendingError struct {
	HILID   string
	Origin  string
	Kind    string
	Profile string
}

func (e *SiteApprovalPendingError) Error() string {
	return fmt.Sprintf("%s is a new site for %s and waits for the user's approval (HIL item %s): ask the user to allow it with `monoagentcli %s hil approve %s` or on the Human in Loop page, then try again",
		e.Origin, KindLabel(e.Kind), e.HILID, ProfileFlag(e.Profile), e.HILID)
}

type siteCard struct {
	Origin      string             `json:"origin"`
	URL         string             `json:"url"`
	Kind        string             `json:"kind"`
	RequestedBy hilsend.PreparedBy `json:"requested_by"`
}

type siteRecord struct {
	Kind      string `json:"kind"`
	AgentKind string `json:"agentKind"`
	Origin    string `json:"origin"`
	DecidedAt string `json:"decidedAt,omitempty"`
}

// IsSiteItem reports whether a hil_pending node_config is a site item's.
func IsSiteItem(nodeConfig string) bool {
	var r struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal([]byte(nodeConfig), &r) == nil && r.Kind == SiteKind
}

// AskSite raises (or reuses the pending one for the same origin and kind)
// the item asking the user to allow rawURL's site for agents of kind.
// by nil reads who asked from this process's env.
func AskSite(ctx context.Context, db *sql.DB, profile, kind, rawURL string, by *hilsend.PreparedBy) (id string, err error) {
	if profile == "" {
		return "", errNoProfile
	}
	origin := Origin(rawURL)
	rows, err := db.QueryContext(ctx, `SELECT id, node_config FROM hil_pending
		WHERE workflow_id = ? AND status = 'pending' AND profile_id = ? ORDER BY rowid`, SiteKind, profile)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var rid, nc string
		if err := rows.Scan(&rid, &nc); err != nil {
			rows.Close()
			return "", err
		}
		var rec siteRecord
		if json.Unmarshal([]byte(nc), &rec) == nil && rec.Origin == origin && rec.AgentKind == kind {
			rows.Close()
			return rid, nil
		}
	}
	rows.Close()
	who := RequestedBy()
	if by != nil {
		who = *by
	}
	id = uuid.NewString()
	ro, _ := json.Marshal(siteCard{Origin: origin, URL: rawURL, Kind: kind, RequestedBy: who})
	nc, _ := json.Marshal(siteRecord{Kind: SiteKind, AgentKind: kind, Origin: origin})
	_, err = db.ExecContext(ctx,
		`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, readonly_data, editable_data, edited_data, node_config, profile_id)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, '{}', '{}', ?, ?)`,
		id, "browser:site:"+id, SiteKind, SiteKind, "Use "+hostOf(rawURL), string(ro), string(nc), profile)
	if err != nil {
		return "", fmt.Errorf("storing the site approval: %w", err)
	}
	return id, nil
}

// SiteItem is one stored site item; Status is pending, approved or rejected.
type SiteItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Origin string `json:"origin"`
	Kind   string `json:"kind"`
}

var errNoProfile = errors.New("a browser approval belongs to a profile, and none was given")

// GetSiteItem reads one site item: its decision is Status.
func GetSiteItem(ctx context.Context, db *sql.DB, profile, id string) (SiteItem, error) {
	it, _, err := findSite(ctx, db, profile, id)
	return it, err
}

func findSite(ctx context.Context, db *sql.DB, profile, id string) (SiteItem, siteRecord, error) {
	if profile == "" {
		return SiteItem{}, siteRecord{}, errNoProfile
	}
	var status, nc string
	err := db.QueryRowContext(ctx, `SELECT status, node_config FROM hil_pending WHERE id = ? AND profile_id = ?`, id, profile).Scan(&status, &nc)
	if errors.Is(err, sql.ErrNoRows) {
		return SiteItem{}, siteRecord{}, ErrSubmitNotFound
	}
	if err != nil {
		return SiteItem{}, siteRecord{}, err
	}
	var rec siteRecord
	if json.Unmarshal([]byte(nc), &rec) != nil || rec.Kind != SiteKind {
		return SiteItem{}, siteRecord{}, fmt.Errorf("that Human-in-Loop item is not a site approval")
	}
	return SiteItem{ID: id, Status: status, Origin: rec.Origin, Kind: rec.AgentKind}, rec, nil
}

// DecideSiteItem approves or rejects a pending site item for the operator
// (`hil approve|reject` call it after refusing an agent). Approving adds an
// allow rule for its origin; rejecting adds nothing.
func DecideSiteItem(ctx context.Context, db *sql.DB, profile, id string, approve bool) (SiteItem, error) {
	it, rec, err := findSite(ctx, db, profile, id)
	if err != nil {
		return it, err
	}
	status := it.Status
	if status != "pending" {
		return it, &SubmitStateError{ID: id, Status: status, Profile: profile}
	}
	to := "rejected"
	if approve {
		to = "approved"
		p, err := LoadPermissions(db)
		if err != nil {
			return it, err
		}
		if p.Rule(rec.Origin) != RuleBlock {
			if _, _, err := p.SetSite(rec.Origin, RuleAllow); err != nil {
				return it, err
			}
			if err := SavePermissions(db, p); err != nil {
				return it, err
			}
		}
	}
	rec.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(rec)
	res, err := db.ExecContext(ctx, `UPDATE hil_pending SET status = ?, node_config = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending' AND profile_id = ?`, to, string(raw), id, profile)
	if err != nil {
		return it, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return it, &SubmitStateError{ID: id, Status: "decided", Profile: profile}
	}
	it.Status = to
	return it, nil
}
