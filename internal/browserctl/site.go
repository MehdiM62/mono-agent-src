package browserctl

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// Site items (plan C8): at level ask, an agent's first visit to a site with
// no rule raises a HIL item asking the user to allow it, instead of a
// refusal. Approving it (operator only, `hil approve` or the HIL page)
// adds an allow rule; rejecting adds nothing. A pending item for the same
// origin and kind is reused; a site the user said no to is not asked again
// for siteRejectMemory, and a profile has at most SiteAskLimit items
// waiting. A row's columns:
//
//	execution_id  "browser:site:<id>" (no workflow waits on it)
//	workflow_id   "browser.site"; node_id the same; node_name "Use <host>"
//	readonly_data {origin,url,kind,requested_by,asciiHost?,unicodeHost?}
//	              (the host's xn-- and Unicode forms when it is an
//	              internationalized name)
//	node_config   {kind:"browser.site", agentKind, origin, decidedAt}

// SiteKind marks a site item: its workflow_id, node_id and node_config kind.
const SiteKind = "browser.site"

// SiteAskLimit is how many site items may wait for the user per profile.
const SiteAskLimit = 20

// siteRejectMemory is how long the user's no to a site holds for a kind.
const siteRejectMemory = 10 * time.Minute

// SiteRejectedError: the user said no to the site for this kind of agent
// within siteRejectMemory; it is refused without asking again. The CLI
// reports it as site_rejected with exit code 3.
type SiteRejectedError struct {
	Origin string
	Kind   string
}

func (e *SiteRejectedError) Error() string {
	return fmt.Sprintf("the user said no to %s for %s in the last %d minutes: don't ask again yet, and tell the user if the task needs that site",
		e.Origin, strings.ToLower(KindLabel(e.Kind)), int(siteRejectMemory.Minutes()))
}

// SiteAsksFullError: SiteAskLimit site items already wait for the user.
// The CLI reports it as site_asks_full with exit code 3.
type SiteAsksFullError struct{ Pending int }

func (e *SiteAsksFullError) Error() string {
	return fmt.Sprintf("%d site approvals already wait for the user: ask them to answer those (the Human in Loop page) before asking for another site", e.Pending)
}

// SiteBlockedDecisionError: approving a site item whose site a block rule
// covers; the item stays pending.
type SiteBlockedDecisionError struct {
	ID     string
	Origin string
}

func (e *SiteBlockedDecisionError) Error() string {
	return fmt.Sprintf("%s is blocked for agents, so item %s can't allow it: remove the block that covers it (Settings → Agent browser permissions, or `monoagentcli browser permissions`) first, or reject the item", e.Origin, e.ID)
}

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
	AsciiHost   string             `json:"asciiHost,omitempty"`
	UnicodeHost string             `json:"unicodeHost,omitempty"`
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
// by nil reads who asked from this process's env. A site the user
// rejected for kind lately is a *SiteRejectedError, and a profile with
// SiteAskLimit items waiting a *SiteAsksFullError; neither raises an item.
func AskSite(ctx context.Context, db *sql.DB, profile, kind, rawURL string, by *hilsend.PreparedBy) (id string, err error) {
	if profile == "" {
		return "", profiledir.ErrNoProfile
	}
	origin := Origin(rawURL)
	pending, err := siteItems(ctx, db, profile, `status = 'pending'`)
	if err != nil {
		return "", err
	}
	for _, it := range pending {
		if it.rec.Origin == origin && it.rec.AgentKind == kind {
			return it.id, nil
		}
	}
	rejected, err := siteItems(ctx, db, profile, `status = 'rejected' AND updated_at >= datetime('now', ?)`,
		fmt.Sprintf("-%d seconds", int(siteRejectMemory.Seconds())))
	if err != nil {
		return "", err
	}
	for _, it := range rejected {
		if it.rec.Origin == origin && it.rec.AgentKind == kind {
			return "", &SiteRejectedError{Origin: origin, Kind: kind}
		}
	}
	if len(pending) >= SiteAskLimit {
		return "", &SiteAsksFullError{Pending: len(pending)}
	}
	who := RequestedBy()
	if by != nil {
		who = *by
	}
	id = uuid.NewString()
	card := siteCard{Origin: origin, URL: rawURL, Kind: kind, RequestedBy: who}
	if u, err := url.Parse(rawURL); err == nil {
		card.AsciiHost, card.UnicodeHost = idnForms(u.Hostname())
	}
	ro, _ := json.Marshal(card)
	nc, _ := json.Marshal(siteRecord{Kind: SiteKind, AgentKind: kind, Origin: origin})
	_, err = db.ExecContext(ctx,
		`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, readonly_data, editable_data, edited_data, node_config, profile_id)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, '{}', '{}', ?, ?)`,
		id, "browser:site:"+id, SiteKind, SiteKind, "Use "+itemHost(rawURL), string(ro), string(nc), profile)
	if err != nil {
		return "", fmt.Errorf("storing the site approval: %w", err)
	}
	return id, nil
}

type storedSite struct {
	id  string
	rec siteRecord
}

// siteItems lists the profile's site items that match where (more SQL
// conditions on hil_pending, with args).
func siteItems(ctx context.Context, db *sql.DB, profile, where string, args ...any) ([]storedSite, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, node_config FROM hil_pending
		WHERE workflow_id = ? AND profile_id = ? AND `+where+` ORDER BY rowid`, append([]any{SiteKind, profile}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedSite
	for rows.Next() {
		var it storedSite
		var nc string
		if err := rows.Scan(&it.id, &nc); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(nc), &it.rec) == nil {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

// SiteItem is one stored site item; Status is pending, approved or rejected.
type SiteItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Origin string `json:"origin"`
	Kind   string `json:"kind"`
}

// GetSiteItem reads one site item: its decision is Status.
func GetSiteItem(ctx context.Context, db *sql.DB, profile, id string) (SiteItem, error) {
	it, _, err := findSite(ctx, db, profile, id)
	return it, err
}

func findSite(ctx context.Context, db *sql.DB, profile, id string) (SiteItem, siteRecord, error) {
	if profile == "" {
		return SiteItem{}, siteRecord{}, profiledir.ErrNoProfile
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
// allow rule for its origin; rejecting adds nothing. A site a block rule
// covers can't be approved (*SiteBlockedDecisionError): the item stays
// pending, for the user to reject or to approve once the block is gone.
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
		if p.Blocked(rec.Origin) {
			return it, &SiteBlockedDecisionError{ID: id, Origin: rec.Origin}
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
	if approve {
		// A block added meanwhile wins: no allow rule then.
		if _, err := UpdatePermissions(db, func(p *Permissions) error {
			if p.Blocked(rec.Origin) {
				return nil
			}
			_, _, err := p.SetSite(rec.Origin, RuleAllow)
			return err
		}); err != nil {
			return it, err
		}
	}
	return it, nil
}
