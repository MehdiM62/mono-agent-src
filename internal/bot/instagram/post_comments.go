//go:build !nosocial

package instagram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Comments: like, reply (WRITE), list (READ)
// ---------------------------------------------------------------------------

// resolveCommentTargetState interprets the result of the comment-target
// lookup scripts shared by LikeComments and ReplyToComment.
//
//   - "marked": a suitable target comment was found and marked.
//   - "not_found": the post shows no comments at all — an error: there is
//     nothing to act on, and reporting success would be a lie.
//   - "author_not_found": commentAuthor was specified but no comment by that
//     author exists. Acting on a different comment would target the wrong
//     user, so this is an error, never a silent fallback to another comment.
func resolveCommentTargetState(state, commentAuthor, postURL, action string) (bool, error) {
	switch state {
	case "marked":
		return true, nil
	case "not_found":
		return false, fmt.Errorf("instagram: no comments on %s to %s", postURL, action)
	case "author_not_found":
		return false, fmt.Errorf("instagram: no comment by author %q found on %s — refusing to %s a different comment", commentAuthor, postURL, action)
	default:
		return false, fmt.Errorf("instagram: unexpected comment lookup state %q on %s", state, postURL)
	}
}

// loadMoreComments clicks "Load more comments" up to rounds times.
func loadMoreComments(ctx context.Context, p browser.PageInterface, rounds int) {
	for i := 0; i < rounds; i++ {
		m := newMark()
		var found bool
		if err := evalJS(p, `(m) => {
			const s = document.querySelector('svg[aria-label="Load more comments"]');
			let b = s && btnOf(s);
			if (!b) b = btns().find((x) => /^(load more comments|view more comments|view all \d+ comments)$/i.test(T(x)));
			if (b) mark(b, m);
			return !!b;
		}`, &found, m); err != nil || !found {
			return
		}
		_ = clickMarked(p, m)
		_ = pause(ctx, scrollSettle)
	}
}

type commentRow struct {
	Author    string `json:"author"`
	Text      string `json:"text"`
	Liked     bool   `json:"liked"`
	Timestamp string `json:"timestamp"`
	HasLike   bool   `json:"hasLike"`
}

// readComments returns the rendered comments.
func readComments(p browser.PageInterface) ([]commentRow, error) {
	var rows []commentRow
	err := evalJS(p, `() => comments().map((c) => ({ author: c.author, text: c.text, liked: c.liked, timestamp: c.timestamp, hasLike: !!c.like }))`, &rows)
	return rows, err
}

// waitComments waits for the comment list (or a post without comments).
func waitComments(ctx context.Context, p browser.PageInterface) []commentRow {
	var rows []commentRow
	_ = poll(ctx, findTimeout, func() (bool, error) {
		var err error
		rows, err = readComments(p)
		if err != nil {
			return false, err
		}
		var bar bool
		_ = evalJS(p, `() => !!commentBox() || postBars().length > 0`, &bar)
		return len(rows) > 0 || bar, nil
	})
	return rows
}

// LikeComments likes comments on a post. With commentAuthor, it likes that
// author's first comment only — never anyone else's; without, it likes up to
// max comments (default 1) in page order. Comments already liked count as
// done and are never toggled. Each like is verified.
func (b *InstagramBot) LikeComments(ctx context.Context, p browser.PageInterface, postURL, commentAuthor string, max int) (map[string]interface{}, error) {
	postURL, err := b.openPost(ctx, p, postURL)
	if err != nil {
		return nil, err
	}
	author := strings.TrimPrefix(strings.TrimSpace(commentAuthor), "@")
	if max <= 0 {
		max = 1
	}
	if author != "" {
		max = 1
	}
	rows := waitComments(ctx, p)
	if author != "" && !hasAuthor(rows, author) {
		loadMoreComments(ctx, p, 3)
		rows, _ = readComments(p)
	}
	state := "marked"
	switch {
	case len(rows) == 0:
		state = "not_found"
	case author != "" && !hasAuthor(rows, author):
		state = "author_not_found"
	}
	if _, err := resolveCommentTargetState(state, author, postURL, "like"); err != nil {
		return nil, err
	}
	liked, already := 0, 0
	var likedAuthors []string
	for i := 0; i < len(rows) && liked < max; i++ {
		r := rows[i]
		if author != "" && !strings.EqualFold(r.Author, author) {
			continue
		}
		if r.Liked {
			already++
			if author != "" {
				break
			}
			continue
		}
		if !r.HasLike {
			if author != "" {
				return nil, fmt.Errorf("instagram: %s's comment on %s has no Like button", author, postURL)
			}
			continue
		}
		if err := likeOneComment(ctx, p, r); err != nil {
			return nil, fmt.Errorf("instagram: like %s's comment on %s: %w", r.Author, postURL, err)
		}
		liked++
		likedAuthors = append(likedAuthors, r.Author)
		if err := pause(ctx, uiPause); err != nil {
			return nil, err
		}
	}
	if liked == 0 && already == 0 {
		return nil, fmt.Errorf("instagram: no likeable comment on %s", postURL)
	}
	status := "liked"
	if liked == 0 {
		status = "already_liked"
	}
	return ok("url", postURL, "status", status, "liked_count", liked, "already_liked_count", already,
		"liked_authors", strings.Join(likedAuthors, ",")), nil
}

func hasAuthor(rows []commentRow, author string) bool {
	for _, r := range rows {
		if strings.EqualFold(r.Author, author) {
			return true
		}
	}
	return false
}

// likeOneComment clicks the like heart of the comment matching r (author and
// text) and verifies it turned into Unlike.
func likeOneComment(ctx context.Context, p browser.PageInterface, r commentRow) error {
	m := newMark()
	var state string
	if err := evalJS(p, `(a, t, m) => {
		const c = comments().find((x) => x.author.toLowerCase() === a.toLowerCase() && x.text === t);
		if (!c) return 'gone';
		if (c.liked) return 'liked';
		if (!c.like) return 'no_button';
		mark(btnOf(c.like) || c.like.parentElement, m);
		return 'marked';
	}`, &state, r.Author, r.Text, m); err != nil {
		return err
	}
	switch state {
	case "liked":
		return nil
	case "marked":
	default:
		return fmt.Errorf("comment %s", state)
	}
	if err := clickMarked(p, m); err != nil {
		return err
	}
	return poll(ctx, verifyTimeout, func() (bool, error) {
		var liked bool
		err := evalJS(p, `(a, t) => { const c = comments().find((x) => x.author.toLowerCase() === a.toLowerCase() && x.text === t); return !!c && c.liked; }`, &liked, r.Author, r.Text)
		return liked, err
	})
}

// ReplyToComment replies to a comment: commentAuthor's first comment, or —
// when commentAuthor is empty — the first comment on the post. The reply is
// only posted once Instagram has put the "@author" reply prefix into the
// composer (so it is threaded under that comment, never posted as a
// top-level comment), and it is verified.
func (b *InstagramBot) ReplyToComment(ctx context.Context, p browser.PageInterface, postURL, commentAuthor, replyText string) (map[string]interface{}, error) {
	if strings.TrimSpace(replyText) == "" {
		return nil, errors.New("instagram: reply text is required")
	}
	postURL, err := b.openPost(ctx, p, postURL)
	if err != nil {
		return nil, err
	}
	author := strings.TrimPrefix(strings.TrimSpace(commentAuthor), "@")
	rows := waitComments(ctx, p)
	if author != "" && !hasAuthor(rows, author) {
		loadMoreComments(ctx, p, 3)
	}
	rm := newMark()
	var target struct {
		State  string `json:"state"`
		Author string `json:"author"`
	}
	if err := evalJS(p, `(a, m) => {
		const cs = comments();
		if (!cs.length) return { state: 'not_found' };
		const c = a ? cs.find((x) => x.author.toLowerCase() === a.toLowerCase()) : cs[0];
		if (!c) return { state: 'author_not_found' };
		mark(c.reply, m);
		return { state: 'marked', author: c.author };
	}`, &target, author, rm); err != nil {
		return nil, err
	}
	if _, err := resolveCommentTargetState(target.State, author, postURL, "reply to"); err != nil {
		return nil, err
	}
	box := newMark()
	if err := b.findCommentBox(ctx, p, box); err != nil {
		return nil, fmt.Errorf("instagram: %s: %w", postURL, err)
	}
	baseline, _ := commentBaseline(p, replyText)
	if err := clickMarked(p, rm); err != nil {
		return nil, fmt.Errorf("instagram: click Reply: %w", err)
	}
	// Instagram answers the click by prefilling "@author " in the composer.
	prefix := "@" + target.Author
	err = poll(ctx, findTimeout/2, func() (bool, error) {
		var v string
		err := evalJS(p, `(m) => { const el = document.querySelector('['+MARK+'="'+m+'"]') || commentBox(); if (el && !el.hasAttribute(MARK)) mark(el, m); return boxValue(el); }`, &v, box)
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), strings.ToLower(prefix)), err
	})
	if err != nil {
		return nil, fmt.Errorf("instagram: Reply did not address %s in the composer on %s — not posting an unthreaded comment", prefix, postURL)
	}
	text := replyText
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), strings.ToLower(prefix)) {
		text = strings.TrimSpace(text[len(prefix):])
	}
	if err := typeAtEnd(ctx, p, box, text); err != nil {
		return nil, fmt.Errorf("instagram: type reply on %s: %w", postURL, err)
	}
	if err := pause(ctx, uiPause/2); err != nil {
		return nil, err
	}
	how, err := b.submitComment(ctx, p, box, text, baseline)
	unmark(p, box)
	if err != nil {
		return nil, fmt.Errorf("instagram: reply on %s not confirmed: %w", postURL, err)
	}
	return ok("url", postURL, "status", "replied", "replied_to", target.Author, "verified_by", how), nil
}

// ListPostComments returns up to maxCount comments of a post: author, text,
// timestamp, is_liked, likes_count and reply_count.
func (b *InstagramBot) ListPostComments(ctx context.Context, p browser.PageInterface, postURL string, maxCount int) ([]map[string]interface{}, error) {
	if maxCount <= 0 {
		maxCount = 50
	}
	postURL, err := b.openPost(ctx, p, postURL)
	if err != nil {
		return nil, err
	}
	waitComments(ctx, p)
	return readCommentList(ctx, p, postURL, maxCount)
}

func readCommentList(ctx context.Context, p browser.PageInterface, postURL string, maxCount int) ([]map[string]interface{}, error) {
	type full struct {
		Author    string `json:"author"`
		Text      string `json:"text"`
		Timestamp string `json:"timestamp"`
		Liked     bool   `json:"is_liked"`
		Likes     int    `json:"likes_count"`
		Replies   int    `json:"reply_count"`
		IsReply   bool   `json:"is_reply"`
	}
	var rows []full
	for round := 0; round < 10; round++ {
		if err := evalJS(p, `() => {
			const cs = comments();
			const depth = (el) => { let n = 0; for (let u = el.closest('ul'); u; u = u.parentElement && u.parentElement.closest('ul')) n++; return n; };
			const min = Math.min(...cs.map((c) => depth(c.el)));
			return cs.map((c) => {
				const likes = (T(c.el).match(/(\d[\d,]*) likes?/i) || [])[1] || '0';
				const li = c.el.closest('li');
				const vr = li ? btns(li).map(T).find((t) => /view (all )?(\d+ )?repl|view replies/i.test(t)) : '';
				const n = vr ? ((vr.match(/(\d+)/) || [])[1] || '0') : '0';
				return { author: c.author, text: c.text, timestamp: c.timestamp, is_liked: c.liked,
					likes_count: parseInt(likes.replace(/,/g, ''), 10) || 0, reply_count: parseInt(n, 10) || 0,
					is_reply: depth(c.el) > min };
			});
		}`, &rows); err != nil {
			return nil, fmt.Errorf("instagram: read comments on %s: %w", postURL, err)
		}
		if len(rows) >= maxCount {
			break
		}
		before := len(rows)
		loadMoreComments(ctx, p, 1)
		var after []commentRow
		after, _ = readComments(p)
		if len(after) <= before {
			break
		}
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		if i >= maxCount {
			break
		}
		out = append(out, map[string]interface{}{
			"platform": "INSTAGRAM", "post_url": postURL, "author": r.Author, "author_url": profileURL(r.Author),
			"text": r.Text, "timestamp": r.Timestamp, "is_liked": r.Liked, "likes_count": r.Likes,
			"reply_count": r.Replies, "is_reply": r.IsReply,
		})
	}
	return out, nil
}
