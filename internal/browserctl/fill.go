package browserctl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
)

// Form fill (`browser fill`): the side panel's Fill form, run on a tab from
// the command line. The profile's Info page and documents answer the page's
// fields; with generate the model may also write an answer to an open
// question. Nothing is submitted: the extension asks the person to press
// Submit. An agent's fill on a page that is not local waits for approval like
// a submit (Gated), since it puts the person's details on a page that can
// read them; at full-no-approvals, and on local pages, it goes through.

// ViaFill marks a held fill (SubmitItem.Via).
const ViaFill = "fill"

// Fill modes: the text fields, or the photo and scan boxes.
const (
	FillText  = "fill"
	FillFiles = "files"
)

// fillTimeout covers the bridge's own form.fill wait (three minutes).
const fillTimeout = 4 * time.Minute

// FillResult is what the extension did.
type FillResult struct {
	Filled    int      `json:"filled"`
	Attached  int      `json:"attached"`
	Found     int      `json:"found"`
	Generated int      `json:"generated"`
	None      bool     `json:"none,omitempty"`
	Sources   []string `json:"sources,omitempty"`
}

func validFillMode(what string) bool { return what == FillText || what == FillFiles }

// FillDigest binds an approval to the page, the profile and what was asked.
func FillDigest(profile, pageURL, what string, generate bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n%t", profile, pagePath(pageURL), what, generate)
	return hex.EncodeToString(h.Sum(nil))
}

// Fill runs the form filler on the session's tab as profile.
func (s *Session) Fill(what string, generate bool) (FillResult, error) {
	if !validFillMode(what) {
		return FillResult{}, fmt.Errorf("fill: unknown mode %q", what)
	}
	params := map[string]interface{}{"profile": s.profile, "what": what}
	if generate {
		params["generate"] = true
	}
	var out FillResult
	if err := s.Call(extension.CmdFormRun, params, fillTimeout, &out); err != nil {
		return FillResult{}, err
	}
	return out, nil
}

func fillSummary(what string, generate bool) FormSummary {
	mode := "text fields"
	if what == FillFiles {
		mode = "photos and scans"
	}
	gen := "no"
	if generate {
		gen = "yes"
	}
	return FormSummary{Action: what, Method: "FILL", Fields: []FormField{
		{Name: "fills", Type: "info", Label: "Fills the page's", Value: mode + ", from this profile's Info page and documents"},
		{Name: "generate", Type: "info", Label: "May write open answers with AI", Value: gen},
	}}
}

// HoldFill records a fill that waits for approval as a Human-in-Loop item and
// returns it as an *AwaitingApproval.
func (s *Session) HoldFill(ctx context.Context, db *sql.DB, what string, generate bool) (*AwaitingApproval, error) {
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{
		Profile: s.profile, URL: s.url, Form: fillSummary(what, generate), Via: ViaFill, TabID: s.tabID,
		Instance: s.info.Instance, Digest: FillDigest(s.profile, s.url, what, generate),
	})
	if err != nil {
		return nil, err
	}
	if path := s.cardScreenshot(it.ID); path != "" {
		_ = setSubmitScreenshot(ctx, db, s.profile, it.ID, path)
	}
	return &AwaitingApproval{HILID: it.ID, Origin: it.Origin, URL: it.URL, Via: ViaFill, Profile: s.profile}, nil
}

// FillFromItem checks an approved fill item against the tab: the same page,
// the same profile. Its errors wrap ErrNotSent.
func FillFromItem(p *extension.ExtensionPage, profile string, it SubmitItem) (what string, generate bool, err error) {
	if it.Via != ViaFill {
		return "", false, fmt.Errorf("%w: item %s is not a fill (use `browser submit --hil %s` or `browser upload --hil %s`)", ErrNotSent, it.ID, it.ID, it.ID)
	}
	if it.FormDigest == "" {
		return "", false, ErrSubmitUnbound
	}
	now, err := p.GetURL()
	if err != nil {
		return "", false, fmt.Errorf("%w: reading the tab: %v", ErrNotSent, err)
	}
	what, generate = it.Form.Action, false
	for _, f := range it.Form.Fields {
		if f.Name == "generate" {
			generate = f.Value == "yes"
		}
	}
	if !validFillMode(what) || FillDigest(profile, now, what, generate) != it.FormDigest {
		return "", false, &SubmitChangedError{What: fmt.Sprintf("the page (approved on %s, now %s)", pagePath(it.URL), pagePath(now))}
	}
	return what, generate, nil
}
