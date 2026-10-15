package browserctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/monoes/mono-agent/internal/extension"
)

// An approval is bound to what the person saw: the page (origin and path)
// and a digest of the form as it stood when the submit was held. Before an
// approved submit is repeated the page is read again, and any difference
// refuses it, so an agent cannot change a field (or go to another page of
// the origin) between the approval and the submit.

// ErrSubmitUnbound is a held submit stored without its form's digest (no
// page to read it from), which nothing can verify.
var ErrSubmitUnbound = fmt.Errorf("%w: this approval was not bound to the form it is for, so it cannot be used: ask for the submit again", ErrNotSent)

// SubmitChangedError is a page or form that is no longer what was approved.
type SubmitChangedError struct{ What string }

func (e *SubmitChangedError) Error() string {
	return fmt.Sprintf("the approved submit was not made: %s changed since it was approved; ask for the submit again", e.What)
}

// Unwrap makes it ErrNotSent: nothing reached the page.
func (e *SubmitChangedError) Unwrap() error { return ErrNotSent }

// held is holdJS's answer.
type held struct {
	Found  bool        `json:"found"`
	Form   FormSummary `json:"form"`
	Digest string      `json:"digest"`
}

// bindForm reads what the held submit f would send on page p (holdJS): the
// masked summary for the card, with f's submitter kept, and the digest the
// approval is bound to.
func bindForm(p *extension.ExtensionPage, f FormSummary) (FormSummary, string, error) {
	if f.Selector == "" && f.Submitter == "" {
		return f, "", errFormGone
	}
	res, err := p.EvalCDP(pageScript(holdJS, f.Selector, f.Submitter, ""))
	if err != nil {
		return f, "", err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return f, "", err
	}
	var h held
	if err := json.Unmarshal(raw, &h); err != nil {
		return f, "", fmt.Errorf("reading the form: %w", err)
	}
	if !h.Found || h.Digest == "" {
		return f, "", errFormGone
	}
	if f.Selector == "" {
		h.Form = f // a control outside any form: nothing to show but the control
	}
	if h.Form.Fields == nil {
		h.Form.Fields = []FormField{}
	}
	h.Form.Submitter, h.Form.Selector = f.Submitter, f.Selector
	return h.Form, h.Digest, nil
}

var errFormGone = errors.New("the form is no longer on the page")

// pagePath is rawURL's origin and path, without query or fragment.
func pagePath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	return Origin(rawURL) + p
}

// VerifyHeld checks that page p is still the page the submit was approved
// on (origin and path) and that the item is bound at all. The form itself
// is checked by resubmit, in the same evaluation as the submit. Its errors
// wrap ErrNotSent.
func VerifyHeld(p *extension.ExtensionPage, it SubmitItem) error {
	if it.FormDigest == "" {
		return ErrSubmitUnbound
	}
	now, err := p.GetURL()
	if err != nil {
		return fmt.Errorf("%w: reading the tab: %v", ErrNotSent, err)
	}
	if pagePath(now) != pagePath(it.URL) {
		return &SubmitChangedError{What: fmt.Sprintf("the page (approved on %s, now %s)", pagePath(it.URL), pagePath(now))}
	}
	return nil
}

// ResubmitItem repeats an approved submit on page p once VerifyHeld passes.
func ResubmitItem(p *extension.ExtensionPage, it SubmitItem) error {
	if err := VerifyHeld(p, it); err != nil {
		return err
	}
	return resubmit(p, it.Form, it.FrameID, it.FormDigest)
}
