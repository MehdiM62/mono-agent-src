//go:build !nosocial

package tiktok

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	botpkg "github.com/monoes/mono-agent/internal/bot"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jevpick"
)

// ---------------------------------------------------------------------------
// Comment on a video
// ---------------------------------------------------------------------------

var commentInputSelectors = []string{
	"[data-e2e='comment-input']",
	"[data-e2e='comment-text']",
}

var commentPostSelectors = []string{
	"[data-e2e='comment-post']",
	"[data-e2e='comment-send-btn']",
	"[data-e2e='comment-post-btn']",
}

// jsFindEditor finds the single visible editor inside the containers sels.
const jsFindEditor = `
const eds = [];
for (const c of all(sels)) {
  const ed = (c.isContentEditable || /^(TEXTAREA|INPUT)$/.test(c.tagName)) ? c : c.querySelector('[contenteditable="true"],textarea,input[type="text"]');
  if (ed && visible(ed) && !eds.includes(ed)) eds.push(ed);
}
if (eds.length !== 1) return {count: eds.length};
mark(eds[0], token);
return {count: 1, text: textOf(eds[0])};
`

const jsResolveEditor = `
const e = document.querySelector(sel);
if (!e) return {count: 0};
const ed = (e.isContentEditable || /^(TEXTAREA|INPUT)$/.test(e.tagName)) ? e : e.querySelector('[contenteditable="true"],textarea');
if (!ed) return {count: 1, rejected: 'not a text box'};
mark(ed, token);
return {count: 1, text: textOf(ed)};
`

// jsFindSubmit finds the submit control nearest to the marked editor.
const jsFindSubmit = `
const ed = marked(edToken);
let found = [];
for (let a = ed && ed.parentElement, i = 0; a && i < 8 && !found.length; a = a.parentElement, i++) found = all(sels, a).map(clickable).filter(visible);
if (!found.length) found = all(sels).map(clickable).filter(visible);
found = found.filter((b, i, a) => a.indexOf(b) === i);
if (found.length !== 1) return {count: found.length};
mark(found[0], token);
return {count: 1, state: disabledOf(found[0]) ? 'disabled' : 'enabled'};
`

const jsResolveSubmit = `
const e = document.querySelector(sel);
if (!e) return {count: 0};
const b = clickable(e);
mark(b, token);
return {count: 1, state: disabledOf(b) ? 'disabled' : 'enabled'};
`

// findEditor locates (and marks with token) a text box by container
// selectors, asking Jev when the selectors are inconclusive.
func (b *TikTokBot) findEditor(ctx context.Context, page browser.PageInterface, sels []string, token, intent string) (toggleFind, error) {
	var f toggleFind
	if err := evalJS(page, "sels, token", jsFindEditor, &f, sels, token); err != nil {
		return f, err
	}
	if f.Count == 1 {
		return f, nil
	}
	if !b.JevAvailable(page) {
		return f, fmt.Errorf("%d matching text boxes", f.Count)
	}
	jf, err := b.jevToggle(ctx, page, jevpick.Target{Kind: "fill", Intent: intent}, jsResolveEditor, token)
	if err != nil {
		return f, fmt.Errorf("%d matching text boxes (jev fallback: %v)", f.Count, err)
	}
	return jf, nil
}

// findSubmit locates (and marks with token) the submit control for the
// editor marked edToken, asking Jev when the selectors are inconclusive, and
// waits for it to become enabled.
func (b *TikTokBot) findSubmit(ctx context.Context, page browser.PageInterface, sels []string, edToken, token, intent string) error {
	var f toggleFind
	find := func() error {
		return evalJS(page, "sels, edToken, token", jsFindSubmit, &f, sels, edToken, token)
	}
	if err := find(); err != nil {
		return err
	}
	jev := false
	if f.Count != 1 {
		if !b.JevAvailable(page) {
			return fmt.Errorf("%d matching submit buttons", f.Count)
		}
		jf, err := b.jevToggle(ctx, page, jevpick.Target{Kind: "click", Intent: intent}, jsResolveSubmit, token)
		if err != nil {
			return fmt.Errorf("%d matching submit buttons (jev fallback: %v)", f.Count, err)
		}
		f, jev = jf, true
	}
	if f.State != "disabled" {
		return nil
	}
	ok, _ := waitFor(ctx, 5*time.Second, func() (bool, error) {
		var r toggleFind
		var err error
		if jev {
			err = evalJS(page, "token", `const e = marked(token); return e ? {count: 1, state: disabledOf(e) ? 'disabled' : 'enabled'} : {count: 0};`, &r, token)
		} else {
			err = evalJS(page, "sels, edToken, token", jsFindSubmit, &r, sels, edToken, token)
		}
		return r.Count == 1 && r.State == "enabled", err
	})
	if !ok {
		return errors.New("submit button stays disabled")
	}
	return nil
}

// typeInto types text into the editor marked token and checks it arrived.
func typeInto(ctx context.Context, page browser.PageInterface, token, text string) error {
	el, err := markedElement(page, token)
	if err != nil {
		return err
	}
	if err := botpkg.TypeInto(ctx, page, el, text); err != nil {
		return err
	}
	want := snippet(text)
	ok, _ := waitFor(ctx, 3*time.Second, func() (bool, error) {
		var got string
		err := evalJS(page, "token", `const e = marked(token); return e ? textOf(e) : '';`, &got, token)
		return strings.Contains(strings.Join(strings.Fields(got), " "), want), err
	})
	if !ok {
		return errors.New("typed text did not appear in the text box")
	}
	return nil
}

const jsCommentProgress = `
const n = commentItems().filter(it => commentInfo(it).text.includes(snip)).length;
const ed = marked(edToken);
return {n, composer: ed ? textOf(ed) : null};
`

type commentProgress struct {
	N        int     `json:"n"`
	Composer *string `json:"composer"`
}

// CommentOnVideo posts commentText on the video at videoURL and confirms it:
// a new comment carrying the text appears, or the composer is cleared by the
// submission. Anything else is an error.
func (b *TikTokBot) CommentOnVideo(ctx context.Context, page browser.PageInterface, videoURL, commentText string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(commentText) == "" {
		return nil, fmt.Errorf("tiktok: commentText is required")
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}

	edTok, postTok := newToken(), newToken()
	defer unmark(page, edTok)
	defer unmark(page, postTok)

	// The composer only renders once the comment panel is open.
	var probe toggleFind
	if err := evalJS(page, "sels, token", jsFindEditor, &probe, commentInputSelectors, edTok); err != nil {
		return nil, fmt.Errorf("tiktok: find comment box: %w", err)
	}
	if probe.Count == 0 {
		if err := openComments(ctx, page); err != nil {
			return nil, fmt.Errorf("%w on %s", err, u)
		}
		_, _ = waitFor(ctx, findTimeout, func() (bool, error) {
			err := evalJS(page, "sels, token", jsFindEditor, &probe, commentInputSelectors, edTok)
			return probe.Count > 0, err
		})
	}
	if _, err := b.findEditor(ctx, page, commentInputSelectors, edTok,
		fmt.Sprintf("the \"Add comment\" text box of the comment section of the TikTok video on this page (%s) — not a direct-message box and not the search box", u)); err != nil {
		return nil, fmt.Errorf("tiktok: comment box not found on %s: %w", u, err)
	}

	snip := snippet(commentText)
	var before commentProgress
	if err := evalJS(page, "snip, edToken", jsCommentProgress, &before, snip, edTok); err != nil {
		return nil, fmt.Errorf("tiktok: read comments: %w", err)
	}
	if err := typeInto(ctx, page, edTok, commentText); err != nil {
		return nil, fmt.Errorf("tiktok: type comment on %s: %w", u, err)
	}
	if err := b.findSubmit(ctx, page, commentPostSelectors, edTok, postTok,
		"the Post button that submits the comment typed into the video's comment box"); err != nil {
		return nil, fmt.Errorf("tiktok: comment Post button on %s: %w", u, err)
	}
	if err := clickMarked(page, postTok); err != nil {
		return nil, fmt.Errorf("tiktok: click Post on %s: %w", u, err)
	}

	var how string
	ok, _ := waitFor(ctx, verifyTimeout, func() (bool, error) {
		var p commentProgress
		if err := evalJS(page, "snip, edToken", jsCommentProgress, &p, snip, edTok); err != nil {
			return false, err
		}
		switch {
		case p.N > before.N:
			how = "comment_rendered"
		case p.Composer != nil && strings.TrimSpace(*p.Composer) == "":
			how = "composer_cleared"
		default:
			return false, nil
		}
		return true, nil
	})
	if !ok {
		return nil, fmt.Errorf("tiktok: comment on %s not confirmed (no new comment and the composer still holds the text)", u)
	}
	return map[string]interface{}{"success": true, "status": "commented", "verified": how, "videoURL": u, "text": commentText}, nil
}
