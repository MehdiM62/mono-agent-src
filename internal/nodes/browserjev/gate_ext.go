package browserjev

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/vault"
)

// guardPage is what the gate needs of the run's tab; *extension.ExtensionPage
// is one.
type guardPage interface {
	SubmitGuard(action string) (*extension.SubmitGuardState, error)
	Screenshot(o extension.ScreenshotOptions) (*extension.Screenshot, error)
	EvalCDP(js string) (interface{}, error)
}

// ArmSubmitGuard arms the extension's submit guard (C1 submit_guard) on the
// run's tab.
func (d extDriver) ArmSubmitGuard(ctx context.Context) (submitGuard, error) {
	if d.page == nil {
		return nil, errors.New("the page has no submit guard (it needs the extension bridge)")
	}
	return armGuard(ctx, d.page)
}

// extGuard is the submit guard of an extension tab. Its approvals are the
// browser.submit items `monoagentcli browser` makes (internal/browserctl),
// so `hil list`, the HIL page and `browser submit --hil` all see them.
type extGuard struct {
	page    guardPage
	db      *sql.DB
	profile string
	seen    int64 // the newest guard record handled
}

func armGuard(ctx context.Context, p guardPage) (*extGuard, error) {
	db := vault.DBFromContext(ctx)
	if db == nil {
		return nil, errors.New("there is no database to hold its approvals")
	}
	st, err := p.SubmitGuard(extension.SubmitGuardArm)
	if err != nil {
		return nil, err
	}
	g := &extGuard{page: p, db: db, profile: vault.ProfileIDFromContext(ctx)}
	for _, b := range st.Blocked {
		g.seen = max(g.seen, b.Seq)
	}
	return g, nil
}

func (g *extGuard) Blocked(ctx context.Context) ([]blockedSubmit, error) {
	st, err := g.page.SubmitGuard(extension.SubmitGuardStatus)
	if err != nil {
		return nil, fmt.Errorf("read the submit guard: %w", err)
	}
	if !st.Armed { // the extension restarted: guard the tab again
		if _, err := g.page.SubmitGuard(extension.SubmitGuardArm); err != nil {
			return nil, fmt.Errorf("re-arm the submit guard: %w", err)
		}
	}
	var newest int64
	for _, b := range st.Blocked {
		newest = max(newest, b.Seq)
	}
	if newest < g.seen {
		g.seen = 0 // its records began again
	}
	var out []blockedSubmit
	for _, b := range st.Blocked {
		if b.Seq > g.seen {
			out = append(out, blockedSubmit{Seq: int(b.Seq), URL: b.URL, Form: formSummary(b.Form)})
		}
	}
	g.seen = max(g.seen, newest)
	return out, nil
}

func formSummary(f extension.SubmitForm) browserctl.FormSummary {
	out := browserctl.FormSummary{Action: f.Action, Method: f.Method, Submitter: f.Submitter, Selector: f.Selector,
		Fields: make([]browserctl.FormField, 0, len(f.Fields))}
	for _, x := range f.Fields {
		out.Fields = append(out.Fields, browserctl.FormField{Name: x.Name, Type: x.Type, Label: x.Label, Value: x.Value})
	}
	return out
}

func (g *extGuard) Ask(ctx context.Context, b blockedSubmit, by hilsend.PreparedBy) (string, error) {
	it, err := browserctl.CreateSubmitItem(ctx, g.db, browserctl.SubmitRequest{
		Profile: g.profile, URL: b.URL, Form: b.Form, Screenshot: g.cardScreenshot(), Via: "guard", TabID: g.TabID(), Seq: b.Seq,
	})
	return it.ID, err
}

func (g *extGuard) Decision(ctx context.Context, hilID string) (string, error) {
	it, err := browserctl.GetSubmitItem(ctx, g.db, g.profile, hilID)
	return it.Status, err
}

func (g *extGuard) Submit(ctx context.Context, b blockedSubmit, hilID string) error {
	if hilID != "" {
		if _, err := browserctl.ClaimSubmitItem(ctx, g.db, g.profile, hilID); err != nil {
			var used *browserctl.SubmitStateError
			if errors.As(err, &used) && used.Status == "consumed" {
				return nil // `browser submit --hil` sent it first
			}
			return err
		}
	}
	err := g.resubmit(b.Form)
	if hilID != "" {
		_ = browserctl.FinishSubmitItem(ctx, g.db, g.profile, hilID, err)
	}
	return err
}

// resubmitJS clicks the recorded submit control, else submits the form.
const resubmitJS = `((sub, form) => {
  const s = sub && document.querySelector(sub);
  if (s) { s.click(); return true; }
  const f = form && document.querySelector(form);
  if (f instanceof HTMLFormElement) { f.requestSubmit(); return true; }
  return false;
})(%s, %s)`

// resubmit lets exactly one submit through and repeats the held one.
func (g *extGuard) resubmit(f browserctl.FormSummary) error {
	if _, err := g.page.SubmitGuard(extension.SubmitGuardAllowOnce); err != nil {
		return fmt.Errorf("let the approved submit through: %w", err)
	}
	sub, _ := json.Marshal(f.Submitter)
	form, _ := json.Marshal(f.Selector)
	res, err := g.page.EvalCDP(fmt.Sprintf(resubmitJS, sub, form))
	if ok, _ := res.(bool); err == nil && !ok {
		err = errors.New("its form is no longer on the page")
	}
	if err != nil {
		_, _ = g.page.SubmitGuard(extension.SubmitGuardArm) // hold the next submit again
		return fmt.Errorf("repeat the approved submit: %w", err)
	}
	return nil
}

// TabID is the guarded tab, for the item and the run's output.
func (g *extGuard) TabID() int {
	if t, ok := g.page.(interface{ TabID() int }); ok {
		return t.TabID()
	}
	return 0
}

// cardScreenshot saves the tab's viewport for the approval card under
// ~/.monoagent/browser/hil/, where `monoagentcli browser` keeps its own;
// "" when it can't.
func (g *extGuard) cardScreenshot() string {
	shot, err := g.page.Screenshot(extension.ScreenshotOptions{})
	if err != nil {
		return ""
	}
	data, err := shot.Bytes()
	home, herr := os.UserHomeDir()
	if err != nil || herr != nil || len(data) == 0 {
		return ""
	}
	dir := filepath.Join(home, ".monoagent", "browser", "hil")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	ext := ".png"
	if shot.MIME == "image/jpeg" {
		ext = ".jpg"
	}
	path := filepath.Join(dir, "jev-"+uuid.NewString()+ext)
	if os.WriteFile(path, data, 0o600) != nil {
		return ""
	}
	return path
}
