//go:build !nosocial

package tiktok

// State-changing TikTok actions. Each one:
//   - reads the current state first and treats "already done" as success
//     without clicking (a like/follow button is a toggle: clicking it on a
//     liked video or a followed account would undo it);
//   - refuses to click when the state cannot be read;
//   - never falls back to "the first matching element" for a targeted control:
//     several matches go to Jev (when the bot has a picker) or fail;
//   - clicks with trusted input and reports success only once the outcome is
//     observable on the page.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jevpick"
)

// toggleFind is what the page-side finders report about a toggle control.
type toggleFind struct {
	Count    int    `json:"count"`
	Pressed  *bool  `json:"pressed"`
	State    string `json:"state"`
	Text     string `json:"text"`
	Rejected string `json:"rejected"`
}

// jevToggle asks Jev for a control, then vets and marks it with token using
// resolveJS (a body over params "sel, token" returning a toggleFind).
func (b *TikTokBot) jevToggle(ctx context.Context, page browser.PageInterface, t jevpick.Target, resolveJS, token string) (toggleFind, error) {
	var f toggleFind
	pick, err := b.JevElement(ctx, page, t)
	if err != nil {
		return f, err
	}
	defer pick.Release()
	if err := evalJS(page, "sel, token", resolveJS, &f, pick.Selector, token); err != nil {
		return f, err
	}
	if f.Rejected != "" {
		return f, fmt.Errorf("jev picked an unsuitable element (%s)", f.Rejected)
	}
	if f.Count != 1 {
		return f, errors.New("jev pick vanished")
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Like a video
// ---------------------------------------------------------------------------

// videoLikeSelectors match the video's own like control (the icon or the
// button around it); comment likes use comment-like-* and never match.
var videoLikeSelectors = []string{
	"[data-e2e='browse-like-icon']",
	"[data-e2e='like-icon']",
	"[data-e2e='browse-like-button']",
	"[data-e2e='like-button']",
}

const jsFindVideoLike = `
let c = [];
for (const e of all(sels)) {
  if (e.closest('[data-e2e*="comment"]')) continue;
  const b = clickable(e);
  if (visible(b) && !c.includes(b)) c.push(b);
}
const inView = c.filter(b => { const r = b.getBoundingClientRect(); return r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth; });
if (inView.length) c = inView;
if (c.length !== 1) return {count: c.length};
mark(c[0], token);
return {count: 1, pressed: pressedOf(c[0])};
`

const jsResolveVideoLike = `
const e = document.querySelector(sel);
if (!e) return {count: 0};
const b = clickable(e);
if (b.closest('[data-e2e*="comment"]')) return {count: 1, rejected: 'belongs to a comment'};
mark(b, token);
return {count: 1, pressed: pressedOf(b)};
`

const jsReadMarkedPressed = `
const e = marked(token);
if (!e) return {count: 0};
return {count: 1, pressed: pressedOf(e)};
`

// LikeVideo likes the video at videoURL. It returns status "already_liked"
// without clicking when the video is liked already, "liked" once the like
// button reports pressed after the click, and an error otherwise.
func (b *TikTokBot) LikeVideo(ctx context.Context, page browser.PageInterface, videoURL string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	sels := videoLikeSelectors
	_, _, _ = page.Race(sels, findTimeout)

	tok := newToken()
	var f toggleFind
	if err := evalJS(page, "sels, token", jsFindVideoLike, &f, sels, tok); err != nil {
		return nil, fmt.Errorf("tiktok: find like button: %w", err)
	}
	if f.Count != 1 {
		if !b.JevAvailable(page) {
			if f.Count == 0 {
				return nil, fmt.Errorf("tiktok: like button not found on %s", u)
			}
			return nil, fmt.Errorf("tiktok: %d candidate like buttons on %s; refusing to guess", f.Count, u)
		}
		jf, jerr := b.jevToggle(ctx, page, jevpick.Target{
			Kind:   "click",
			Intent: fmt.Sprintf("the Like (heart) button of the TikTok video playing on this page (%s) — the video's own like heart in its action bar, not a comment's like button and not another video's", u),
			Hint:   "TikTok labels it e.g. \"Like video\"; aria-pressed=true means the video is already liked.",
		}, jsResolveVideoLike, tok)
		if jerr != nil {
			return nil, fmt.Errorf("tiktok: like button not found on %s (%d selector matches; jev fallback: %v)", u, f.Count, jerr)
		}
		f = jf
	}
	defer unmark(page, tok)
	if f.Pressed == nil {
		return nil, fmt.Errorf("tiktok: cannot tell whether %s is already liked (no aria-pressed); refusing to click a toggle", u)
	}
	if *f.Pressed {
		return map[string]interface{}{"success": true, "status": "already_liked", "videoURL": u}, nil
	}
	if err := clickMarked(page, tok); err != nil {
		return nil, fmt.Errorf("tiktok: click like on %s: %w", u, err)
	}
	ok, _ := waitFor(ctx, verifyTimeout, func() (bool, error) {
		var r toggleFind
		err := evalJS(page, "token", jsReadMarkedPressed, &r, tok)
		return r.Count == 1 && r.Pressed != nil && *r.Pressed, err
	})
	if !ok {
		return nil, fmt.Errorf("tiktok: like on %s not confirmed (button never reported pressed)", u)
	}
	return map[string]interface{}{"success": true, "status": "liked", "videoURL": u}, nil
}

// ---------------------------------------------------------------------------
// Like a comment
// ---------------------------------------------------------------------------

// maxCommentScrolls bounds how far LikeComment scrolls a long comment list.
const maxCommentScrolls = 40

// jsFindComment finds the one comment matching (id) or (username, text).
const jsFindComment = `
const want = norm(text).toLowerCase(), wantUser = (user || '').replace(/^@/, '').toLowerCase();
const matches = [], byID = [];
for (const it of commentItems()) {
  const c = commentInfo(it);
  if (id && c.id && c.id === id) { byID.push(it); continue; }
  if (!want) continue;
  if (!c.text.toLowerCase().includes(want)) continue;
  if (wantUser && c.username.toLowerCase() !== wantUser) continue;
  matches.push(it);
}
// An id match wins; a derived id (tth-…) is shared by one author's identical
// comments, and several matches are refused like several text matches.
const hits = byID.length ? byID : matches;
if (hits.length !== 1) return {count: hits.length};
const it = hits[0];
mark(it, itemToken);
const like = commentLike(it);
if (!like) return {count: 1, state: 'no_button'};
mark(like, token);
return {count: 1, pressed: pressedOf(like)};
`

const jsResolveCommentLike = `
const e = document.querySelector(sel);
if (!e) return {count: 0};
const item = marked(itemToken);
const b = clickable(e);
const inside = item && item.contains(b) ? b : (item && item.contains(e) ? e : null);
if (!inside) return {count: 1, rejected: 'not inside the target comment'};
mark(inside, token);
return {count: 1, pressed: pressedOf(inside)};
`

// LikeComment likes one comment on the video at videoURL. The comment is
// identified by commentID when TikTok exposes one in the DOM, otherwise by
// its text (a unique substring) and, when given, its author's username. It
// returns status "already_liked" without clicking when it is liked already.
// Zero or several matching comments is an error — never another comment.
func (b *TikTokBot) LikeComment(ctx context.Context, page browser.PageInterface, videoURL, commentID, username, text string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	commentID, username, text = strings.TrimSpace(commentID), strings.TrimSpace(username), snippet(text)
	if commentID == "" && text == "" {
		return nil, fmt.Errorf("tiktok: like_comment needs the comment's text (and ideally its username) or an id")
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	if err := openComments(ctx, page); err != nil {
		return nil, fmt.Errorf("%w on %s", err, u)
	}
	desc := fmt.Sprintf("comment %q", text)
	if username != "" {
		desc = fmt.Sprintf("comment by @%s %q", strings.TrimPrefix(username, "@"), text)
	}
	if text == "" {
		desc = fmt.Sprintf("comment %s", commentID)
	}

	tok, itemTok := newToken(), newToken()
	find := func() (toggleFind, error) {
		var f toggleFind
		err := evalJS(page, "id, user, text, token, itemToken", jsFindComment, &f, commentID, username, text, tok, itemTok)
		return f, err
	}
	// Scroll the comment list until the comment shows up (or the list ends).
	var f toggleFind
	prev, stale := -1, 0
	for round := 0; ; round++ {
		f, err = find()
		if err != nil {
			return nil, fmt.Errorf("tiktok: find %s: %w", desc, err)
		}
		if f.Count > 0 || round >= maxCommentScrolls {
			break
		}
		var n int
		if err := evalJS(page, "", `return commentItems().length;`, &n); err != nil {
			return nil, fmt.Errorf("tiktok: count comments: %w", err)
		}
		if n == prev {
			if stale++; stale >= maxStaleRounds {
				break
			}
		} else {
			stale = 0
		}
		prev = n
		if err := scrollComments(page); err != nil {
			return nil, err
		}
		if err := pause(ctx, 1200*time.Millisecond); err != nil {
			return nil, err
		}
	}
	defer unmark(page, itemTok)
	defer unmark(page, tok)
	switch {
	case f.Count == 0:
		return nil, fmt.Errorf("tiktok: %s not found on %s", desc, u)
	case f.Count > 1:
		return nil, fmt.Errorf("tiktok: %d comments match %s on %s; refusing to guess (pass the username too)", f.Count, desc, u)
	}
	if f.State == "no_button" {
		if !b.JevAvailable(page) {
			return nil, fmt.Errorf("tiktok: like button of %s not found on %s", desc, u)
		}
		jf, jerr := b.jevToggle(ctx, page, jevpick.Target{
			Kind:   "click",
			Intent: fmt.Sprintf("the like (heart) button of the %s under the TikTok video — that comment's own like button, not the video's like button and not another comment's", desc),
		}, jsResolveCommentLikeWithItem(itemTok), tok)
		if jerr != nil {
			return nil, fmt.Errorf("tiktok: like button of %s not found on %s (jev fallback: %v)", desc, u, jerr)
		}
		f = jf
	}
	if f.Pressed == nil {
		return nil, fmt.Errorf("tiktok: cannot tell whether %s is already liked; refusing to click a toggle", desc)
	}
	result := map[string]interface{}{"success": true, "videoURL": u, "commentID": commentID, "username": username, "text": text}
	if *f.Pressed {
		result["status"] = "already_liked"
		return result, nil
	}
	if err := clickMarked(page, tok); err != nil {
		return nil, fmt.Errorf("tiktok: click like of %s: %w", desc, err)
	}
	ok, _ := waitFor(ctx, verifyTimeout, func() (bool, error) {
		var r toggleFind
		err := evalJS(page, "token", jsReadMarkedPressed, &r, tok)
		return r.Count == 1 && r.Pressed != nil && *r.Pressed, err
	})
	if !ok {
		return nil, fmt.Errorf("tiktok: like of %s not confirmed", desc)
	}
	result["status"] = "liked"
	return result, nil
}

// jsResolveCommentLikeWithItem binds the comment's item token into the
// resolve script (jevToggle passes only sel and token).
func jsResolveCommentLikeWithItem(itemTok string) string {
	return "const itemToken = " + jsString(itemTok) + ";\n" + jsResolveCommentLike
}

// jsString renders s as a JavaScript string literal.
func jsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, " ", ` `, " ", ` `, "<", `\x3c`)
	return "'" + r.Replace(s) + "'"
}

// ---------------------------------------------------------------------------
// Follow
// ---------------------------------------------------------------------------

var followSelectors = []string{
	"[data-e2e='follow-button']",
	"[data-e2e='follow-btn']",
}

// jsFollowState classifies a follow button; shared by the finder and resolver.
const jsFollowState = `
const scoped = (e) => !e.closest('[data-e2e*="suggest"],[data-e2e*="recommend"],[data-e2e*="user-card"]');
const classify = (b) => {
  const p = b.getAttribute('aria-pressed');
  const t = norm(b.innerText || b.textContent || b.getAttribute('aria-label')).toLowerCase();
  if (p === 'true' || /^(following|friends|unfollow)$/.test(t)) return 'following';
  if (/^requested$/.test(t)) return 'requested';
  if (/^(follow|follow back)$/.test(t) || p === 'false') return 'not_following';
  return 'unknown';
};
`

const jsFindFollow = jsFollowState + `
const btns = all(sels).map(clickable).filter((b, i, a) => a.indexOf(b) === i && visible(b) && scoped(b));
if (btns.length === 1) { mark(btns[0], token); return {count: 1, state: classify(btns[0]), text: norm(btns[0].innerText)}; }
const unf = [...document.querySelectorAll('[data-e2e="unfollow-button"]')].filter(e => visible(e) && scoped(e));
if (btns.length === 0 && unf.length === 1) return {count: 1, state: 'following'};
return {count: btns.length};
`

const jsResolveFollow = jsFollowState + `
const e = document.querySelector(sel);
if (!e) return {count: 0};
const b = clickable(e);
if (!scoped(b)) return {count: 1, rejected: 'belongs to a suggested account'};
mark(b, token);
return {count: 1, state: classify(b), text: norm(b.innerText)};
`

const jsReadFollow = jsFollowState + `
const e = marked(token);
if (e && visible(e)) return {count: 1, state: classify(e)};
const btns = all(sels).map(clickable).filter((b, i, a) => a.indexOf(b) === i && visible(b) && scoped(b));
if (btns.length === 1) return {count: 1, state: classify(btns[0])};
const unf = [...document.querySelectorAll('[data-e2e="unfollow-button"]')].filter(e => visible(e) && scoped(e));
if (unf.length === 1) return {count: 1, state: 'following'};
return {count: btns.length};
`

// FollowUser follows the account at profile (URL or handle). It returns
// status "already_following" (or "already_requested") without clicking when
// the account is followed already, "followed"/"requested" once the button
// reports the new state, and an error when the state cannot be read or the
// follow is not confirmed.
func (b *TikTokBot) FollowUser(ctx context.Context, page browser.PageInterface, profile string) (map[string]interface{}, error) {
	u, handle, err := profileTarget(profile)
	if err != nil {
		return nil, err
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	_, _, _ = page.Race(append(append([]string{}, followSelectors...), "[data-e2e='unfollow-button']"), findTimeout)

	tok := newToken()
	var f toggleFind
	if err := evalJS(page, "sels, token", jsFindFollow, &f, followSelectors, tok); err != nil {
		return nil, fmt.Errorf("tiktok: find follow button: %w", err)
	}
	if f.Count != 1 {
		if !b.JevAvailable(page) {
			if f.Count == 0 {
				return nil, fmt.Errorf("tiktok: follow button not found on %s", u)
			}
			return nil, fmt.Errorf("tiktok: %d candidate follow buttons on %s; refusing to guess", f.Count, u)
		}
		jf, jerr := b.jevToggle(ctx, page, jevpick.Target{
			Kind:   "click",
			Intent: fmt.Sprintf("the Follow button of the TikTok profile shown on this page (@%s) — the profile owner's own Follow button in the profile header, not a Follow button of a suggested account", handle),
			Hint:   "If the button reads Following or Friends the account is already followed.",
		}, jsResolveFollow, tok)
		if jerr != nil {
			return nil, fmt.Errorf("tiktok: follow button not found on %s (%d selector matches; jev fallback: %v)", u, f.Count, jerr)
		}
		f = jf
	}
	defer unmark(page, tok)
	res := map[string]interface{}{"success": true, "profileURL": u, "username": handle}
	switch f.State {
	case "following":
		res["status"] = "already_following"
		return res, nil
	case "requested":
		res["status"] = "already_requested"
		return res, nil
	case "not_following":
	default:
		return nil, fmt.Errorf("tiktok: cannot read the follow state of %s (button %q); refusing to click a toggle", u, f.Text)
	}
	if err := clickMarked(page, tok); err != nil {
		return nil, fmt.Errorf("tiktok: click follow on %s: %w", u, err)
	}
	var state string
	ok, _ := waitFor(ctx, verifyTimeout, func() (bool, error) {
		var r toggleFind
		err := evalJS(page, "sels, token", jsReadFollow, &r, followSelectors, tok)
		state = r.State
		return r.State == "following" || r.State == "requested", err
	})
	if !ok {
		return nil, fmt.Errorf("tiktok: follow of %s not confirmed (state %q)", u, state)
	}
	if state == "requested" {
		res["status"] = "requested"
	} else {
		res["status"] = "followed"
	}
	return res, nil
}
