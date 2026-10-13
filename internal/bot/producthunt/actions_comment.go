//go:build !nosocial

package producthunt

import (
	"context"
	"fmt"
	"strings"
	"time"

	botpkg "github.com/monoes/mono-agent/internal/bot"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jevpick"
)

// ---------------------------------------------------------------------------
// comment_on_launch
// ---------------------------------------------------------------------------

const formMark = "data-monoagent-ph-form"

// markComposerJS tags the launch's top-level composer (a comment-form that
// is not a reply form inside a thread) and reports its state.
const markComposerJS = `(mark) => {
	document.querySelectorAll('[' + mark + ']').forEach(e => e.removeAttribute(mark));
	const form = Array.from(document.querySelectorAll('form[data-test="comment-form"]')).find(f => !f.closest('[data-test^="thread-"]'));
	const signIn = !!document.querySelector('[data-test="header-nav-link-sign-in"]');
	if (!form) return { found: false, signIn };
	form.setAttribute(mark, '1');
	const btn = form.querySelector('[data-test="form-submit-button"], button[type="submit"]');
	return { found: true, signIn, button: btn ? btn.textContent.replace(/\s+/g, ' ').trim() : '',
		editor: !!form.querySelector('[contenteditable="true"], textarea') };
}`

type composerState struct {
	Found  bool   `json:"found"`
	SignIn bool   `json:"signIn"`
	Button string `json:"button"`
	Editor bool   `json:"editor"`
}

// newCommentJS finds a comment that was not on the page before (prior ids)
// whose own body contains needle.
const newCommentJS = `(prior, needle) => {
	const isC = el => /^comment-[0-9]+$/.test(el.getAttribute('data-test') || '');
	const owner = x => { for (let e = x; e; e = e.parentElement) if (e.nodeType === 1 && isC(e)) return e; return null; };
	const seen = new Set(prior);
	for (const el of document.querySelectorAll('[data-test^="comment-"]')) {
		if (!isC(el)) continue;
		const id = el.getAttribute('data-test').slice(8);
		if (seen.has(id)) continue;
		const body = Array.from(el.querySelectorAll('.prose, [class*="richText"]')).find(x => owner(x) === el);
		const t = body ? (body.innerText || body.textContent || '') : '';
		if (t.replace(/\s+/g, '').toLowerCase().includes(needle)) return { id };
	}
	return { id: '' };
}`

func isLoginLabel(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "login") || strings.Contains(s, "log in") ||
		strings.Contains(s, "sign in") || strings.Contains(s, "sign up")
}

// elementText reads what an input control holds: value for textarea/input,
// textContent for a contenteditable editor.
func elementText(el browser.ElementHandle) string {
	if v, err := el.Property("value"); err == nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v, err := el.Property("textContent"); err == nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// CommentOnLaunch posts a top-level comment on a launch and confirms it: a
// comment that was not on the page before, whose text is the typed text,
// must appear. The composer is the launch's own comment form (never a reply
// form), its submit button is looked up inside that form (never a
// "Comments" tab elsewhere), and a "Login to comment" button means the
// session is logged out — nothing is typed or clicked then.
func (b *ProductHuntBot) CommentOnLaunch(ctx context.Context, page browser.PageInterface, launchURL, text string) (map[string]interface{}, error) {
	u, err := checkLaunchURL(launchURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("producthunt: comment text is required")
	}
	if err := open(page, u); err != nil {
		return nil, err
	}
	_ = commentSection(page)

	var cs composerState
	if err := botpkg.EvalJSON(page, markComposerJS, &cs, formMark); err != nil {
		return nil, fmt.Errorf("producthunt: reading the comment form: %w", err)
	}
	if isLoginLabel(cs.Button) || (!cs.Found && cs.SignIn) {
		return nil, fmt.Errorf("producthunt: not logged in (the comment form asks to log in)")
	}
	form := "[" + formMark + "]"
	editorSel := form + " [contenteditable='true']"
	editorAlts := []string{form + " textarea"}

	var editor browser.ElementHandle
	var release = func() {}
	defer func() { release() }()
	if cs.Found && !cs.Editor {
		// The composer starts as a placeholder; clicking it opens the editor.
		if ph, _, err := botpkg.FindFirst(page, form+" p", []string{form + " > div"}, 2*time.Second); err == nil {
			_ = botpkg.ClickTrusted(page, ph)
		}
	}
	if cs.Found {
		editor, _, err = botpkg.FindFirst(page, editorSel, editorAlts, controlWait)
	}
	if editor == nil {
		if !b.JevAvailable(page) {
			return nil, fmt.Errorf("producthunt: comment box not found on %s: %v", u, err)
		}
		pick, jerr := b.JevElement(ctx, page, jevpick.Target{
			Kind:   "fill",
			Intent: "the text box for writing a new top-level comment on this Product Hunt launch (placeholder like \"What do you think?\") — not a reply box under an existing comment, not the search box",
		})
		if jerr != nil {
			return nil, fmt.Errorf("producthunt: comment box not found on %s: %v (jev fallback: %v)", u, err, jerr)
		}
		editor, release = pick.Element, pick.Release
	}

	before, err := readComments(page)
	if err != nil {
		return nil, err
	}
	prior := make([]string, 0, len(before))
	for _, c := range before {
		prior = append(prior, c.ID)
	}

	if err := botpkg.TypeInto(ctx, page, editor, text); err != nil {
		return nil, fmt.Errorf("producthunt: typing the comment: %w", err)
	}
	if got := elementText(editor); !strings.Contains(normText(got), normText(text)) {
		return nil, fmt.Errorf("producthunt: the comment box holds %q after typing — not submitting", got)
	}

	var submit browser.ElementHandle
	if cs.Found {
		submit, _, err = botpkg.FindFirst(page, form+" [data-test='form-submit-button']", []string{form + " button[type='submit']"}, controlWait)
	}
	if submit == nil {
		if !b.JevAvailable(page) {
			return nil, fmt.Errorf("producthunt: comment submit button not found: %v", err)
		}
		pick, jerr := b.JevElement(ctx, page, jevpick.Target{
			Kind:   "click",
			Intent: "the button that posts the comment just typed into the launch's comment box (labelled e.g. \"Comment\") — not a \"Comments\" tab, not a Reply button, not an Upvote button",
		})
		if jerr != nil {
			return nil, fmt.Errorf("producthunt: comment submit button not found: %v (jev fallback: %v)", err, jerr)
		}
		defer pick.Release()
		submit = pick.Element
	}
	if lbl, err := submit.Text(); err == nil && isLoginLabel(lbl) {
		return nil, fmt.Errorf("producthunt: not logged in (the submit button reads %q)", lbl)
	}
	if err := botpkg.ClickTrusted(page, submit); err != nil {
		return nil, fmt.Errorf("producthunt: clicking the comment submit button: %w", err)
	}

	want := needle(text)
	deadline := time.Now().Add(confirmTimeout)
	for {
		var found struct {
			ID string `json:"id"`
		}
		if err := botpkg.EvalJSON(page, newCommentJS, &found, prior, want); err == nil && found.ID != "" {
			return map[string]interface{}{"success": true, "launchURL": u, "commentID": found.ID}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("producthunt: comment not confirmed — it did not appear on %s within %v", u, confirmTimeout)
		}
		if err := sleepCtx(ctx, pollInterval); err != nil {
			return nil, err
		}
	}
}
