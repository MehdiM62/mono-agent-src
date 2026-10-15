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

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/hilsend"
)

// Submit items: a form submit an agent tried on a non-local origin, held
// in hil_pending (so `hil list`, the HIL page and the badge show it) until
// a person approves it. A row's columns:
//
//	execution_id  "browser:<id>" — no workflow execution waits on it, so an
//	              approval resumes nothing
//	workflow_id   "browser.submit"; node_id the same; node_name "Submit on <host>"
//	status        pending → approved (`hil approve`) or rejected →
//	              consumed (`browser submit --hil`, once)
//	readonly_data {origin,url,form,screenshot,requested_by}
//	editable_data {}
//	node_config   the submit record (kind "browser.submit"): how to re-trigger it

// SubmitKind marks a submit item: its workflow_id, node_id and node_config kind.
const SubmitKind = "browser.submit"

// Errors of the submit item store.
var (
	ErrSubmitNotFound = errors.New("submit approval not found")
	ErrNotSubmitItem  = errors.New("that Human-in-Loop item is not a browser submit")
	// ErrApprovedElsewhere is an approval set by a path an agent can reach
	// (the MCP server's or the HTTP API's hil tools), not by the operator.
	ErrApprovedElsewhere = errors.New("this submit was approved outside `monoagentcli hil approve` and the Human in Loop page, so it cannot be used: ask for the submit again and have the user approve it there")
)

// SubmitStateError is a submit item that cannot be used in its state.
type SubmitStateError struct {
	ID      string
	Status  string
	Profile string
}

func (e *SubmitStateError) Error() string {
	switch e.Status {
	case "pending":
		return fmt.Sprintf("submit %s is not approved yet: a person approves it with `monoagentcli %s hil approve %s` or on the Human in Loop page", e.ID, ProfileFlag(e.Profile), e.ID)
	case "rejected":
		return fmt.Sprintf("submit %s was rejected", e.ID)
	case "consumed":
		return fmt.Sprintf("submit %s was already used: one approval allows one submit", e.ID)
	}
	return fmt.Sprintf("submit %s is %s", e.ID, e.Status)
}

// FormField is one field of a form summary; sensitive values are "•••".
type FormField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
}

// FormSummary describes the form a submit would send.
type FormSummary struct {
	Action string      `json:"action"`
	Method string      `json:"method"`
	Fields []FormField `json:"fields"`
	// Submitter is a CSS selector for the control that submits it ("" when
	// the page submitted it by script); Selector finds the form itself.
	Submitter string `json:"submitter,omitempty"`
	Selector  string `json:"selector,omitempty"`
}

// SubmitRequest is a held submit to record.
type SubmitRequest struct {
	// RequestedBy names who asked for the card; nil reads it from this
	// process's env (RequestedBy()).
	RequestedBy *hilsend.PreparedBy
	// Page is the tab the submit was held on. The form is read from it
	// again (bindForm), and the item is bound to that reading's digest;
	// without a page, or when the form cannot be read, the item is unbound
	// and can never be used (VerifyHeld).
	Page       *extension.ExtensionPage
	Profile    string
	URL        string
	Form       FormSummary
	Screenshot string
	// Via is "click" (the agent clicked a submit control) or "guard" (the
	// page's submit_guard blocked it); Seq is the guard's record.
	Via      string
	TabID    int
	Instance string
	Seq      int
	// FrameID is the frame the submit was held in (submit_guard's record);
	// 0, the top frame, is the only one a held submit can be repeated in.
	FrameID int
}

// ApprovedByOperator is SubmitItem.ApprovedBy for an approval made through
// DecideSubmitItem, i.e. `hil approve` or the Human in Loop page, which
// refuse an agent.
const ApprovedByOperator = "operator"

// SubmitItem is one stored submit item.
type SubmitItem struct {
	ID          string             `json:"id"`
	Status      string             `json:"status"`
	Origin      string             `json:"origin"`
	URL         string             `json:"url"`
	Form        FormSummary        `json:"form"`
	Screenshot  string             `json:"screenshot,omitempty"`
	RequestedBy hilsend.PreparedBy `json:"requested_by"`
	Via         string             `json:"via"`
	TabID       int                `json:"tabId"`
	Instance    string             `json:"instance,omitempty"`
	Seq         int                `json:"seq,omitempty"`
	FrameID     int                `json:"frameId,omitempty"`
	FormDigest  string             `json:"formDigest,omitempty"`
	ApprovedBy  string             `json:"approvedBy,omitempty"`
	ConsumedAt  string             `json:"consumedAt,omitempty"`
	SubmittedAt string             `json:"submittedAt,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type submitCard struct {
	Origin      string             `json:"origin"`
	URL         string             `json:"url"`
	Form        FormSummary        `json:"form"`
	Screenshot  string             `json:"screenshot,omitempty"`
	RequestedBy hilsend.PreparedBy `json:"requested_by"`
}

type submitRecord struct {
	Kind        string `json:"kind"`
	Via         string `json:"via"`
	TabID       int    `json:"tabId"`
	Instance    string `json:"instance,omitempty"`
	Seq         int    `json:"seq,omitempty"`
	FrameID     int    `json:"frameId,omitempty"`
	FormDigest  string `json:"formDigest,omitempty"`
	ApprovedBy  string `json:"approvedBy,omitempty"`
	DecidedAt   string `json:"decidedAt,omitempty"`
	ConsumedAt  string `json:"consumedAt,omitempty"`
	SubmittedAt string `json:"submittedAt,omitempty"`
	Error       string `json:"error,omitempty"`
}

// IsSubmitItem reports whether a hil_pending node_config is a submit item's.
func IsSubmitItem(nodeConfig string) bool {
	var r struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal([]byte(nodeConfig), &r) == nil && r.Kind == SubmitKind
}

// CreateSubmitItem stores a pending submit item and returns it.
func CreateSubmitItem(ctx context.Context, db *sql.DB, req SubmitRequest) (SubmitItem, error) {
	if req.Profile == "" {
		req.Profile = "default"
	}
	digest := ""
	if req.Page != nil && req.FrameID == 0 {
		if f, d, err := bindForm(req.Page, req.Form); err == nil {
			req.Form, digest = f, d
		}
	}
	if req.Form.Fields == nil {
		req.Form.Fields = []FormField{}
	}
	who := RequestedBy()
	if req.RequestedBy != nil {
		who = *req.RequestedBy
	}
	it := SubmitItem{
		ID: uuid.NewString(), Status: "pending", Origin: Origin(req.URL), URL: req.URL, Form: req.Form,
		Screenshot: req.Screenshot, RequestedBy: who, Via: req.Via, TabID: req.TabID,
		Instance: req.Instance, Seq: req.Seq, FrameID: req.FrameID, FormDigest: digest,
	}
	ro, err := json.Marshal(submitCard{Origin: it.Origin, URL: it.URL, Form: it.Form, Screenshot: it.Screenshot, RequestedBy: it.RequestedBy})
	if err != nil {
		return it, err
	}
	nc, err := json.Marshal(submitRecord{Kind: SubmitKind, Via: it.Via, TabID: it.TabID, Instance: it.Instance, Seq: it.Seq,
		FrameID: it.FrameID, FormDigest: it.FormDigest})
	if err != nil {
		return it, err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, readonly_data, editable_data, edited_data, node_config, profile_id)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, '{}', '{}', ?, ?)`,
		it.ID, "browser:"+it.ID, SubmitKind, SubmitKind, "Submit on "+hostOf(req.URL), string(ro), string(nc), req.Profile)
	if err != nil {
		return it, fmt.Errorf("storing the submit approval: %w", err)
	}
	return it, nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "a page"
}

// GetSubmitItem reads one submit item by id or unique id prefix.
func GetSubmitItem(ctx context.Context, db *sql.DB, profile, id string) (SubmitItem, error) {
	it, _, err := findSubmit(ctx, db, profile, id)
	return it, err
}

func findSubmit(ctx context.Context, db *sql.DB, profile, id string) (SubmitItem, submitRecord, error) {
	id = strings.TrimSpace(id)
	if profile == "" {
		profile = "default"
	}
	if id == "" {
		return SubmitItem{}, submitRecord{}, ErrSubmitNotFound
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, status, readonly_data, node_config FROM hil_pending
		 WHERE profile_id = ? AND (id = ? OR substr(id, 1, ?) = ?) LIMIT 3`, profile, id, len(id), id)
	if err != nil {
		return SubmitItem{}, submitRecord{}, err
	}
	defer rows.Close()
	type row struct{ id, status, ro, nc string }
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.status, &r.ro, &r.nc); err != nil {
			return SubmitItem{}, submitRecord{}, err
		}
		if r.id == id {
			found = []row{r}
			break
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return SubmitItem{}, submitRecord{}, err
	}
	if len(found) != 1 {
		return SubmitItem{}, submitRecord{}, ErrSubmitNotFound
	}
	r := found[0]
	var rec submitRecord
	if json.Unmarshal([]byte(r.nc), &rec) != nil || rec.Kind != SubmitKind {
		return SubmitItem{}, submitRecord{}, ErrNotSubmitItem
	}
	var card submitCard
	if err := json.Unmarshal([]byte(r.ro), &card); err != nil {
		return SubmitItem{}, rec, fmt.Errorf("reading the submit approval: %w", err)
	}
	return SubmitItem{
		ID: r.id, Status: r.status, Origin: card.Origin, URL: card.URL, Form: card.Form, Screenshot: card.Screenshot,
		RequestedBy: card.RequestedBy, Via: rec.Via, TabID: rec.TabID, Instance: rec.Instance, Seq: rec.Seq,
		FrameID: rec.FrameID, FormDigest: rec.FormDigest, ApprovedBy: rec.ApprovedBy, ConsumedAt: rec.ConsumedAt, SubmittedAt: rec.SubmittedAt, Error: rec.Error,
	}, rec, nil
}

// DecideSubmitItem approves or rejects a pending item for the operator:
// `hil approve|reject` call it after refusing an agent, and an approval is
// stamped ApprovedByOperator. Other ways to set the row's status (the MCP
// server's and the HTTP API's hil tools) leave no stamp, so their approval
// cannot be used.
func DecideSubmitItem(ctx context.Context, db *sql.DB, profile, id string, approve bool) (SubmitItem, error) {
	it, rec, err := findSubmit(ctx, db, profile, id)
	if err != nil {
		return it, err
	}
	if it.Status != "pending" {
		return it, &SubmitStateError{ID: it.ID, Status: it.Status, Profile: profile}
	}
	to := "rejected"
	if approve {
		to, rec.ApprovedBy = "approved", ApprovedByOperator
	}
	rec.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	if err := updateSubmit(ctx, db, profile, it.ID, "pending", to, rec); err != nil {
		if errors.Is(err, errRaced) {
			return GetSubmitItem(ctx, db, profile, it.ID)
		}
		return it, err
	}
	it.Status, it.ApprovedBy = to, rec.ApprovedBy
	return it, nil
}

// ClaimSubmitItem uses an approved item once: approved → consumed. Any
// other state is a *SubmitStateError, an approval without the operator's
// stamp ErrApprovedElsewhere; of two concurrent claims one wins.
func ClaimSubmitItem(ctx context.Context, db *sql.DB, profile, id string) (SubmitItem, error) {
	it, rec, err := findSubmit(ctx, db, profile, id)
	if err != nil {
		return it, err
	}
	if it.Status != "approved" {
		return it, &SubmitStateError{ID: it.ID, Status: it.Status, Profile: profile}
	}
	if rec.ApprovedBy != ApprovedByOperator {
		return it, ErrApprovedElsewhere
	}
	rec.ConsumedAt = time.Now().UTC().Format(time.RFC3339)
	if err := updateSubmit(ctx, db, profile, it.ID, "approved", "consumed", rec); err != nil {
		if errors.Is(err, errRaced) {
			return it, &SubmitStateError{ID: it.ID, Status: "consumed", Profile: profile}
		}
		return it, err
	}
	it.Status, it.ConsumedAt = "consumed", rec.ConsumedAt
	return it, nil
}

// ReleaseSubmitItem hands a claimed item back (consumed → approved) when
// nothing reached the page, so the approval is not lost.
func ReleaseSubmitItem(ctx context.Context, db *sql.DB, profile, id string) error {
	_, rec, err := findSubmit(ctx, db, profile, id)
	if err != nil {
		return err
	}
	rec.ConsumedAt = ""
	return updateSubmit(ctx, db, profile, id, "consumed", "approved", rec)
}

// FinishSubmitItem records how the re-triggered submit went.
func FinishSubmitItem(ctx context.Context, db *sql.DB, profile, id string, runErr error) error {
	_, rec, err := findSubmit(ctx, db, profile, id)
	if err != nil {
		return err
	}
	if runErr != nil {
		rec.Error = runErr.Error()
	} else {
		rec.SubmittedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return updateSubmit(ctx, db, profile, id, "consumed", "consumed", rec)
}

var errRaced = errors.New("the item changed meanwhile")

func updateSubmit(ctx context.Context, db *sql.DB, profile, id, from, to string, rec submitRecord) error {
	if profile == "" {
		profile = "default"
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx,
		`UPDATE hil_pending SET status = ?, node_config = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND status = ? AND profile_id = ?`, to, string(raw), id, from, profile)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errRaced
	}
	return nil
}
