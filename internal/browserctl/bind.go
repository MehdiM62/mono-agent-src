package browserctl

import (
	"crypto/sha256"
	"encoding/hex"
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

// FormDigest is the digest an approval is bound to: the form's action,
// method, and each field's name, type and value (masked values as masked),
// in order. Labels are left out: they are not sent.
func FormDigest(f FormSummary) string {
	type field struct{ Name, Type, Value string }
	doc := struct {
		Action, Method string
		Fields         []field
	}{Action: f.Action, Method: f.Method, Fields: []field{}}
	for _, x := range f.Fields {
		doc.Fields = append(doc.Fields, field{x.Name, x.Type, x.Value})
	}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// summarizeForm reads the form at selector on page p (summarizeFormJS);
// found is false when no form matches.
func summarizeForm(p *extension.ExtensionPage, selector string) (FormSummary, bool, error) {
	var out struct {
		Found bool        `json:"found"`
		Form  FormSummary `json:"form"`
	}
	res, err := p.EvalCDP(withSelector(summarizeFormJS, selector))
	if err != nil {
		return FormSummary{}, false, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return FormSummary{}, false, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return FormSummary{}, false, fmt.Errorf("reading the form: %w", err)
	}
	if out.Form.Fields == nil {
		out.Form.Fields = []FormField{}
	}
	return out.Form, out.Found, nil
}

// bindForm reads f's form on page p the way VerifyHeld will, keeping f's
// submitter, and returns it with its digest. A submit with no form (a
// submit-like control outside any form) is bound to the page alone.
func bindForm(p *extension.ExtensionPage, f FormSummary) (FormSummary, string, error) {
	if f.Selector == "" {
		return f, FormDigest(FormSummary{}), nil
	}
	got, found, err := summarizeForm(p, f.Selector)
	if err != nil {
		return f, "", err
	}
	if !found {
		return f, "", errFormGone
	}
	got.Submitter = f.Submitter
	return got, FormDigest(got), nil
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

// VerifyHeld checks that page p still shows what it was approved for: the
// same origin and path, and a form with the approved digest. Its errors
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
	digest := FormDigest(FormSummary{})
	if it.Form.Selector != "" {
		got, found, err := summarizeForm(p, it.Form.Selector)
		if err != nil {
			return fmt.Errorf("%w: reading the form: %v", ErrNotSent, err)
		}
		if !found {
			return &SubmitChangedError{What: "the form (it is no longer on the page)"}
		}
		digest = FormDigest(got)
	}
	if digest != it.FormDigest {
		return &SubmitChangedError{What: "the form's contents"}
	}
	return nil
}

// ResubmitItem repeats an approved submit on page p once VerifyHeld passes.
func ResubmitItem(p *extension.ExtensionPage, it SubmitItem) error {
	if err := VerifyHeld(p, it); err != nil {
		return err
	}
	return resubmit(p, it.Form, it.FrameID)
}
