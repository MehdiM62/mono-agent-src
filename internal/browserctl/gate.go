package browserctl

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
)

// The submit gate (C2): an agent's form submit on an allowed origin that is
// not local waits for a person. Before an agent acts on such a page the
// page's submit_guard is armed; a click on a submit-like control is held
// instead of performed, and submits the guard blocked (Enter in a field,
// script) are reported on the command that caused them or the next one.
// Each becomes a HIL item (hil.go); after approval the agent runs
// `browser submit --hil <id>`. Fetch/XHR-only "forms" are not caught.

// AwaitingApproval is a submit held for a person's approval. The CLI
// reports it as {status:"awaiting_approval",hil_id} with exit code 3.
type AwaitingApproval struct {
	HILID  string `json:"hil_id"`
	Origin string `json:"origin"`
	URL    string `json:"url"`
	Via    string `json:"via"`
}

func (a *AwaitingApproval) Error() string {
	return fmt.Sprintf("the form submit on %s waits for a person's approval (HIL item %s): ask the user to approve it with `monoagentcli hil approve %s` or on the Human in Loop page, then run `monoagentcli browser submit --hil %s`",
		a.Origin, a.HILID, a.HILID, a.HILID)
}

// Gated reports whether the gate applies to the session's tab now.
func (s *Session) Gated() bool { return s.tabID > 0 && s.policy.SubmitGated(s.url) }

// guardStatus is submit_guard's answer.
type guardStatus struct {
	Armed   bool           `json:"armed"`
	Blocked []blockedEntry `json:"blocked"`
}

type blockedEntry struct {
	Seq  int         `json:"seq"`
	TS   float64     `json:"ts"`
	URL  string      `json:"url"`
	Form FormSummary `json:"form"`
}

func (s *Session) guard(action string) (guardStatus, error) {
	var st guardStatus
	err := s.Call(CmdSubmitGuard, map[string]interface{}{"action": action}, DefaultTimeout, &st)
	return st, err
}

// BeforeAct arms the guard on a gated tab and reports a submit it blocked
// since the last command (an *AwaitingApproval). It fails closed: a gated
// tab whose guard cannot be armed is not acted on.
func (s *Session) BeforeAct(ctx context.Context, db *sql.DB) error {
	if !s.Gated() {
		return nil
	}
	if _, err := s.guard("arm"); err != nil {
		return fmt.Errorf("this page needs approval before any form is submitted, and its submit guard could not be armed (update the extension?): %w", err)
	}
	return s.takeBlocked(ctx, db)
}

// AfterAct reports a submit the guard blocked during the action. The
// action may have navigated the tab, so its URL is read again first.
func (s *Session) AfterAct(ctx context.Context, db *sql.DB) error {
	if s.tabID == 0 {
		return nil
	}
	was := s.Gated()
	if _, err := s.Refresh(); err != nil {
		return nil // the tab went away or is navigating; the next command checks again
	}
	if !was && !s.Gated() {
		return nil
	}
	return s.takeBlocked(ctx, db)
}

// takeBlocked turns guard records newer than the last reported one into
// HIL items and returns the first as an *AwaitingApproval.
func (s *Session) takeBlocked(ctx context.Context, db *sql.DB) error {
	st, err := s.guard("status")
	if err != nil {
		return fmt.Errorf("reading the page's submit guard: %w", err)
	}
	key := tabKey(s.info.Instance, s.tabID)
	last := s.state.Reported[key]
	maxSeq := 0
	for _, b := range st.Blocked {
		if b.Seq > maxSeq {
			maxSeq = b.Seq
		}
	}
	if maxSeq < last {
		last = 0 // the extension restarted and its seq began again
	}
	var first *AwaitingApproval
	for _, b := range st.Blocked {
		if b.Seq <= last {
			continue
		}
		u := b.URL
		if u == "" {
			u = s.url
		}
		a, err := s.Hold(ctx, db, u, b.Form, "guard", b.Seq)
		if err != nil {
			return err
		}
		if first == nil {
			first = a
		}
	}
	if maxSeq != s.state.Reported[key] {
		if s.state.Reported == nil {
			s.state.Reported = map[string]int{}
		}
		s.state.Reported[key] = maxSeq
		_ = s.saveState()
	}
	if first != nil {
		return first
	}
	return nil
}

// Hold records a held submit on pageURL as a HIL item, with a screenshot
// of the page for the card, and returns it as an *AwaitingApproval.
func (s *Session) Hold(ctx context.Context, db *sql.DB, pageURL string, form FormSummary, via string, seq int) (*AwaitingApproval, error) {
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{
		Profile: s.profile, URL: pageURL, Form: form, Via: via, TabID: s.tabID, Instance: s.info.Instance, Seq: seq,
	})
	if err != nil {
		return nil, err
	}
	if path := s.cardScreenshot(it.ID); path != "" {
		it.Screenshot = path
		_ = setSubmitScreenshot(ctx, db, s.profile, it.ID, path)
	}
	return &AwaitingApproval{HILID: it.ID, Origin: it.Origin, URL: it.URL, Via: via}, nil
}

// cardScreenshot saves the tab's viewport for a HIL card under
// ~/.monoagent/browser/hil/<id>.png; "" when it cannot.
func (s *Session) cardScreenshot(id string) string {
	shot, err := s.Screenshot(ScreenshotOptions{})
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".monoagent", "browser", "hil")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	path := filepath.Join(dir, safeKey(id)+shot.Ext())
	if os.WriteFile(path, shot.Data, 0o600) != nil {
		return ""
	}
	return path
}

func setSubmitScreenshot(ctx context.Context, db *sql.DB, profile, id, path string) error {
	if profile == "" {
		profile = "default"
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT readonly_data FROM hil_pending WHERE id = ? AND profile_id = ?`, id, profile).Scan(&raw); err != nil {
		return err
	}
	var card submitCard
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return err
	}
	card.Screenshot = path
	b, err := json.Marshal(card)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE hil_pending SET readonly_data = ? WHERE id = ? AND profile_id = ?`, string(b), id, profile)
	return err
}

// SubmitTarget is what a click on selector would do to a form.
type SubmitTarget struct {
	Found  bool        `json:"found"`
	Submit bool        `json:"submit"`
	Why    string      `json:"why,omitempty"`
	Form   FormSummary `json:"form"`
}

// ProbeSubmit reports whether clicking selector is submit-like: an
// [type=submit] control, a button inside a <form>, or a control whose text
// says submit, send, post, publish, pay, buy, confirm, order, sign up or
// apply. The form it would send is summarized with sensitive values masked.
func (s *Session) ProbeSubmit(selector string) (SubmitTarget, error) {
	var t SubmitTarget
	sel, _ := json.Marshal(selector)
	res, err := s.Page().EvalCDP(fmt.Sprintf(probeSubmitJS, string(sel)))
	if err != nil {
		return t, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("checking the click target: %w", err)
	}
	if t.Form.Fields == nil {
		t.Form.Fields = []FormField{}
	}
	t.Form.Submitter = selector
	return t, nil
}

// ErrNotSent wraps a Resubmit failure that happened before anything
// reached the page, so the approval can be handed back.
var ErrNotSent = errors.New("nothing was submitted")

// Resubmit performs an approved submit item on the session's tab: the
// guard lets exactly one submit through, then the recorded submitter is
// clicked, or the form submitted when there was none.
func (s *Session) Resubmit(it SubmitItem) error {
	if it.Form.Submitter == "" && it.Form.Selector == "" {
		return fmt.Errorf("%w: the held submit recorded neither its submit control nor its form, so it cannot be repeated", ErrNotSent)
	}
	if _, err := s.guard("allow_once"); err != nil {
		return fmt.Errorf("%w: letting the submit through: %v", ErrNotSent, err)
	}
	if it.Form.Submitter != "" {
		_, err := s.Send(extension.CmdClick, map[string]interface{}{"selector": it.Form.Submitter}, DefaultTimeout)
		return err
	}
	sel, _ := json.Marshal(it.Form.Selector)
	res, err := s.Page().EvalCDP(fmt.Sprintf(`(() => { const f = document.querySelector(%s);
  if (!(f instanceof HTMLFormElement)) return false; f.requestSubmit(); return true; })()`, string(sel)))
	if err != nil {
		return err
	}
	if ok, _ := res.(bool); !ok {
		return fmt.Errorf("the form %s is no longer on the page", it.Form.Selector)
	}
	return nil
}

// settle gives a page a moment to act on a click before AfterAct reads it.
const settle = 300 * time.Millisecond

// Settle waits briefly so a submit the action set off reaches the guard.
func Settle() { time.Sleep(settle) }
