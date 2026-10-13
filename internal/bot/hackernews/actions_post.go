//go:build !nosocial

package hackernews

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	botpkg "github.com/monoes/mono-agent/internal/bot"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jevpick"
)

// SubmitPost submits a story through /submit and confirms it by finding the
// new item on the logged-in user's /submitted?id=<user> page: a story that
// was not listed before the submit and carries the submitted title (or
// link). HN answers a duplicate URL by redirecting to the existing item and
// a rate limit with a bare text page; both are errors. Once the form has
// been submitted, a failed read-back is reported as an error that says so —
// the action never resubmits.
func (b *HackerNewsBot) SubmitPost(ctx context.Context, page browser.PageInterface, title, linkURL, text string) (map[string]interface{}, error) {
	title = strings.TrimSpace(title)
	linkURL = strings.TrimSpace(linkURL)
	if title == "" {
		return nil, fmt.Errorf("hackernews: title is required")
	}
	if linkURL != "" {
		u, err := url.Parse(linkURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("hackernews: url must be an absolute http(s) URL, got %q", linkURL)
		}
	}

	if err := open(page, hnOrigin+"/submit"); err != nil {
		return nil, err
	}
	st, err := readState(page)
	if err != nil {
		return nil, err
	}
	if st.User == "" {
		if p := st.refusal(); p != "" {
			return nil, fmt.Errorf("hackernews: submit page refused: %s", p)
		}
		return nil, fmt.Errorf("hackernews: not logged in (no #me on the submit page)")
	}
	user := st.User

	before, err := readSubmitted(page, user)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range before {
		known[r.ID] = true
	}

	if err := open(page, hnOrigin+"/submit"); err != nil {
		return nil, err
	}
	titleEl, release, err := b.findControl(ctx, page, "form input[name='title']", nil, controlWait, jevpick.Target{
		Kind: "fill", Intent: "the title field of the Hacker News submit form",
	})
	if err != nil {
		return nil, fmt.Errorf("hackernews: title field not found: %w", err)
	}
	defer release()
	if err := typeAndCheck(ctx, page, titleEl, title, "title"); err != nil {
		return nil, err
	}
	if linkURL != "" {
		urlEl, rel, err := b.findControl(ctx, page, "form input[name='url']", nil, controlWait, jevpick.Target{
			Kind: "fill", Intent: "the url field of the Hacker News submit form",
		})
		if err != nil {
			return nil, fmt.Errorf("hackernews: url field not found: %w", err)
		}
		defer rel()
		if err := typeAndCheck(ctx, page, urlEl, linkURL, "url"); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(text) != "" {
		textEl, rel, err := b.findControl(ctx, page, "form textarea[name='text']", nil, controlWait, jevpick.Target{
			Kind: "fill", Intent: "the text box of the Hacker News submit form",
		})
		if err != nil {
			return nil, fmt.Errorf("hackernews: text field not found: %w", err)
		}
		defer rel()
		if err := typeAndCheck(ctx, page, textEl, text, "text"); err != nil {
			return nil, err
		}
	}

	submit, rel, err := b.findControl(ctx, page, "form:has(input[name='title']) input[type='submit']",
		[]string{"form:has(input[name='title']) button[type='submit']"}, controlWait, jevpick.Target{
			Kind: "click", Intent: "the submit button of the Hacker News submit form",
		})
	if err != nil {
		return nil, fmt.Errorf("hackernews: submit button not found: %w", err)
	}
	defer rel()

	// From here on the post may exist: every failure says so, and nothing
	// submits again.
	const noRetry = "the form was submitted; not resubmitting to avoid a double post — check " // + URL
	navErr := botpkg.PageClickAndWaitNavigation(ctx, page, submit, 30*time.Second)
	after, err := readState(page)
	if err != nil {
		return nil, fmt.Errorf("hackernews: submission not confirmed (%v); %s%s", err, noRetry, submittedURL(user))
	}
	if p := after.refusal(); p != "" {
		return nil, fmt.Errorf("hackernews: Hacker News refused the submission: %s", p)
	}
	if navErr != nil {
		return nil, fmt.Errorf("hackernews: submission not confirmed (%v); %s%s", navErr, noRetry, submittedURL(user))
	}
	if u, perr := url.Parse(after.URL); perr == nil {
		switch u.Path {
		case "/item":
			return nil, fmt.Errorf("hackernews: the URL was already submitted — Hacker News redirected to existing item %s", u.Query().Get("id"))
		case "/submit", "/r":
			if has, _ := page.Has("form input[name='title']"); has {
				return nil, fmt.Errorf("hackernews: Hacker News rejected the submission: %s", after.Text)
			}
		}
	}

	wantTitle := normText(title)
	for attempt := 0; attempt < readBackAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, readBackDelay); err != nil {
				return nil, err
			}
		}
		rows, err := readSubmitted(page, user)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if known[r.ID] || !itemIDRe.MatchString(r.ID) {
				continue
			}
			if normText(r.Title) == wantTitle || (linkURL != "" && sameLink(r.Link, linkURL)) {
				return map[string]interface{}{
					"id":     r.ID,
					"title":  r.Title,
					"url":    itemURL(r.ID),
					"link":   r.Link,
					"author": user,
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("hackernews: submission not confirmed — no new item titled %q on /submitted; %s%s", title, noRetry, submittedURL(user))
}

// ---------------------------------------------------------------------------
// reply_to_comment
// ---------------------------------------------------------------------------

// buildReplyURL builds the URL for an item's reply form. itemID is passed
// through net/url's query encoding rather than being interpolated into the
// URL with fmt.Sprintf, so a hostile itemID (e.g. containing "&", "#", or
// other reserved URL characters) can never corrupt the query string or
// smuggle extra parameters — it is always confined to the "id" and "goto"
// query values, however it's spelled.
func buildReplyURL(itemID string) string {
	v := url.Values{}
	v.Set("id", itemID)
	v.Set("goto", "item?id="+itemID)
	return hnOrigin + "/reply?" + v.Encode()
}

type postedComment struct {
	ID string `json:"id"`
}

// findMyCommentJS finds, on the page HN redirected to after a reply, a
// comment by user whose normalised text contains needle and whose id is
// greater than the parent's (ids grow over time). The newest match wins.
const findMyCommentJS = `(user, needle, parent) => {
	const norm = s => s.replace(/[*\s]+/g, '').toLowerCase();
	let best = null;
	for (const row of document.querySelectorAll('tr.athing.comtr')) {
		const a = row.querySelector('a.hnuser');
		const body = row.querySelector('.commtext');
		if (!a || !body || a.textContent.trim() !== user) continue;
		if (!/^[0-9]+$/.test(row.id) || (parent && BigInt(row.id) <= BigInt(parent))) continue;
		const c = body.cloneNode(true);
		c.querySelectorAll('.reply').forEach(e => e.remove());
		if (!norm(c.textContent).includes(needle)) continue;
		if (!best || BigInt(row.id) > BigInt(best)) best = row.id;
	}
	return { id: best || '' };
}`

// ReplyToComment replies to an item (story or comment) through its reply
// form, then confirms the reply: after the submit HN redirects to the item,
// where a comment by the logged-in user with the typed text must appear.
// Rate-limit and login pages are reported as errors.
func (b *HackerNewsBot) ReplyToComment(ctx context.Context, page browser.PageInterface, itemID, text string) (map[string]interface{}, error) {
	id, err := checkItemID(itemID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("hackernews: reply text is required")
	}
	if err := open(page, buildReplyURL(id)); err != nil {
		return nil, err
	}
	st, err := readState(page)
	if err != nil {
		return nil, err
	}
	if st.User == "" {
		if p := st.refusal(); p != "" {
			return nil, fmt.Errorf("hackernews: reply page refused: %s", p)
		}
		return nil, fmt.Errorf("hackernews: not logged in (no #me on the reply page)")
	}
	if p := st.refusal(); p != "" {
		return nil, fmt.Errorf("hackernews: reply page refused: %s", p)
	}

	box, release, err := b.findControl(ctx, page, "form[action='comment'] textarea[name='text']",
		[]string{"form:has(input[name='parent']) textarea"}, controlWait, jevpick.Target{
			Kind: "fill", Intent: fmt.Sprintf("the reply text box on this Hacker News reply page (replying to item %s)", id),
		})
	if err != nil {
		return nil, fmt.Errorf("hackernews: reply box not found on %s: %w", st.URL, err)
	}
	defer release()
	if err := typeAndCheck(ctx, page, box, text, "reply"); err != nil {
		return nil, err
	}
	submit, rel, err := b.findControl(ctx, page, "form[action='comment'] input[type='submit']",
		[]string{"form:has(input[name='parent']) input[type='submit']"}, controlWait, jevpick.Target{
			Kind: "click", Intent: "the submit ('reply') button of the Hacker News reply form",
		})
	if err != nil {
		return nil, fmt.Errorf("hackernews: reply button not found: %w", err)
	}
	defer rel()

	navErr := botpkg.PageClickAndWaitNavigation(ctx, page, submit, 30*time.Second)
	after, err := readState(page)
	if err != nil {
		return nil, fmt.Errorf("hackernews: reply not confirmed: %w", err)
	}
	if p := after.refusal(); p != "" {
		return nil, fmt.Errorf("hackernews: Hacker News refused the reply: %s", p)
	}
	if navErr != nil {
		return nil, fmt.Errorf("hackernews: reply not confirmed: %w", navErr)
	}

	want := needle(text)
	deadline := time.Now().Add(confirmTimeout)
	for {
		var found postedComment
		if err := botpkg.EvalJSON(page, findMyCommentJS, &found, st.User, want, id); err == nil && found.ID != "" {
			return map[string]interface{}{
				"success":   true,
				"itemID":    id,
				"commentID": found.ID,
				"url":       itemURL(found.ID),
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("hackernews: reply not confirmed — no comment by %s with the reply text on %s", st.User, after.URL)
		}
		if err := sleepCtx(ctx, pollInterval); err != nil {
			return nil, err
		}
	}
}
