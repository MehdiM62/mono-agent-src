//go:build !nosocial

package instagram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Followers / following (READ)
// ---------------------------------------------------------------------------

// scrollSettle is how long list readers wait for lazy content after a scroll.
var scrollSettle = 1500 * time.Millisecond

// FetchFollowersList opens profile's followers (or following) dialog and
// reads up to maxCount accounts from it, scrolling the dialog for more.
// sourceType is "followers"/"FOLLOWERS_FETCH" or "following"/
// "FOLLOWING_FETCH"; when profile is empty, a URL or username in sourceType
// is taken as the profile (followers).
func (b *InstagramBot) FetchFollowersList(ctx context.Context, p browser.PageInterface, profile, sourceType string, maxCount int) ([]map[string]interface{}, error) {
	kind := "followers"
	st := strings.ToLower(strings.TrimSpace(sourceType))
	switch {
	case strings.Contains(st, "following"):
		kind = "following"
	case st == "" || strings.Contains(st, "follower"):
	default:
		if strings.TrimSpace(profile) == "" {
			profile = sourceType
		}
	}
	if listKindRe.MatchString(strings.TrimSpace(profile)) {
		profile = "" // a template fallback handed the list kind as the profile
	}
	if strings.TrimSpace(profile) == "" {
		return nil, errors.New("instagram: no profile given for the followers list (target_url, profileUrl, or a profile URL in sourceType)")
	}
	if maxCount <= 0 {
		maxCount = 100
	}
	username, err := b.openProfile(ctx, p, profile)
	if err != nil {
		return nil, err
	}
	if _, err := waitProfileHeader(ctx, p); err != nil {
		return nil, fmt.Errorf("instagram: %s's profile did not render: %w", username, err)
	}
	var private bool
	_ = evalJS(p, `() => /this account is private/i.test(VT())`, &private)
	if private {
		return nil, fmt.Errorf("instagram: %s is private — its %s list is not visible", username, kind)
	}
	dialogOpen := func() (bool, error) {
		var n int
		err := evalJS(p, `() => { const d = dialogs().find((x) => x.querySelector('a[href]')); return d ? d.querySelectorAll('a[href]').length : 0; }`, &n)
		return n > 0, err
	}
	// clickCount clicks the header's "<n> followers" (or following) count.
	// Profiles of other accounts may carry a real /<user>/followers/ link;
	// the 2026 header — always on your own profile — has
	// <a role="link" href="#"> in plain <div>s, found by its text.
	clickCount := func() bool {
		m := newMark()
		defer unmark(p, m)
		var found bool
		if err := evalJS(p, `(kind, m) => {
			const h = hdr();
			if (!h) return false;
			const re = new RegExp('/' + kind + '/?$');
			let a = Array.from(h.querySelectorAll('a[href]')).find((x) => re.test(x.getAttribute('href') || ''));
			if (!a) { const s = headerStats(h)[kind]; a = s ? s.target : null; }
			if (a) mark(a, m);
			return !!a;
		}`, &found, kind, m); err != nil || !found {
			return false
		}
		return clickMarked(p, m) == nil
	}
	opened := clickCount() && poll(ctx, findTimeout, dialogOpen) == nil
	if !opened {
		// The /<user>/followers/ route opens the dialog on some profiles —
		// not on your own; there, click the count again on the reloaded page.
		if err := b.open(ctx, p, profileURL(username)+kind+"/"); err != nil {
			return nil, err
		}
		opened = poll(ctx, findTimeout, dialogOpen) == nil
		if !opened {
			if _, err := waitProfileHeader(ctx, p); err == nil && clickCount() {
				opened = poll(ctx, findTimeout, dialogOpen) == nil
			}
		}
		if !opened {
			return nil, fmt.Errorf("instagram: %s dialog did not open on %s's profile (no clickable %s count in the header, and the /%s/ route did not open it)", kind, username, kind, kind)
		}
	}
	var results []map[string]interface{}
	seen := map[string]bool{}
	prev, stale := 0, 0
	for round := 0; round < 200 && len(results) < maxCount; round++ {
		var batch []struct {
			Username string `json:"username"`
			FullName string `json:"full_name"`
			Image    string `json:"image"`
			Verified bool   `json:"verified"`
		}
		if err := evalJS(p, `(owner) => {
			const d = dialogs().find((x) => x.querySelector('a[href]'));
			if (!d) return [];
			const out = [], seen = new Set();
			for (const a of d.querySelectorAll('a[href]')) {
				const u = userOf(a.getAttribute('href'));
				if (!u || u.toLowerCase() === owner.toLowerCase() || seen.has(u)) continue;
				seen.add(u);
				// The row: the nearest ancestor holding a single account.
				let row = a;
				while (row.parentElement && row.parentElement !== d &&
					new Set(Array.from(row.parentElement.querySelectorAll('a[href]')).map((x) => userOf(x.getAttribute('href'))).filter(Boolean)).size <= 1) row = row.parentElement;
				const names = Array.from(row.querySelectorAll('span[dir="auto"], span')).map(T).filter((t) => t && t !== u && !/^(follow|following|remove|requested|·)$/i.test(t));
				const img = row.querySelector('img');
				out.push({ username: u, full_name: names.find((t) => !t.includes(u)) || '', image: img ? img.getAttribute('src') || '' : '',
					verified: !!row.querySelector('svg[aria-label="Verified"]') });
			}
			return out;
		}`, &batch, username); err != nil {
			return nil, fmt.Errorf("instagram: read %s dialog: %w", kind, err)
		}
		for _, u := range batch {
			if seen[u.Username] {
				continue
			}
			seen[u.Username] = true
			results = append(results, map[string]interface{}{
				"platform": "INSTAGRAM", "username": u.Username, "url": profileURL(u.Username),
				"profile_url": profileURL(u.Username), "full_name": u.FullName, "image_url": u.Image,
				"is_verified": u.Verified, "source": kind, "source_profile": username,
			})
		}
		if len(results) >= maxCount {
			break
		}
		prev, stale = trackStagnation(len(results), prev, stale)
		if stale >= 3 {
			break
		}
		var scrolled bool
		_ = evalJS(p, `() => {
			const d = dialogs().find((x) => x.querySelector('a[href]'));
			if (!d) return false;
			const els = [d].concat(Array.from(d.querySelectorAll('div')));
			const sc = els.filter((e) => e.scrollHeight > e.clientHeight + 4 && /(auto|scroll)/.test(getComputedStyle(e).overflowY));
			const el = sc.sort((a, b) => b.scrollHeight - a.scrollHeight)[0] || d;
			el.scrollTop = el.scrollHeight;
			el.dispatchEvent(new Event('scroll'));
			return true;
		}`, &scrolled)
		if err := pause(ctx, scrollSettle); err != nil {
			return results, err
		}
	}
	if len(results) > maxCount {
		results = results[:maxCount]
	}
	return results, nil
}

// trackStagnation updates the scroll-progress counters used to detect when a
// scrollable list has stopped loading new items. It returns the new prevCount
// and noChangeRounds to carry into the next iteration.
func trackStagnation(currentCount, prevCount, noChangeRounds int) (newPrevCount, newNoChangeRounds int) {
	if currentCount == prevCount {
		return prevCount, noChangeRounds + 1
	}
	return currentCount, 0
}

// ---------------------------------------------------------------------------
// Post grids: a profile's posts, search results (READ)
// ---------------------------------------------------------------------------

type postLink struct {
	URL       string `json:"url"`
	Shortcode string `json:"shortcode"`
	Kind      string `json:"kind"`
	Author    string `json:"author"`
	Thumbnail string `json:"thumbnail_src"`
	Alt       string `json:"alt_text"`
}

// collectPostLinks reads post/reel links from the page's main content
// (never the header's highlights), scrolling for more until maxCount or the
// grid stops growing.
func collectPostLinks(ctx context.Context, p browser.PageInterface, maxCount int, defaultAuthor string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	seen := map[string]bool{}
	prev, stale := 0, 0
	for round := 0; round < 100 && len(out) < maxCount; round++ {
		var links []postLink
		if err := evalJS(p, `() => {
			const root = document.querySelector('main') || document.body;
			const out = [];
			for (const a of root.querySelectorAll('a[href]')) {
				if (a.closest('header') || a.closest('[role="dialog"]') || !vis(a)) continue;
				const ref = postRef(a.getAttribute('href'));
				if (!ref) continue;
				const img = a.querySelector('img');
				out.push({ url: ref.url, shortcode: ref.shortcode, kind: ref.kind, author: ref.author,
					thumbnail_src: img ? img.getAttribute('src') || '' : '', alt_text: img ? img.getAttribute('alt') || '' : '' });
			}
			return out;
		}`, &links); err != nil {
			return out, err
		}
		for _, l := range links {
			if seen[l.Shortcode] || len(out) >= maxCount {
				continue
			}
			seen[l.Shortcode] = true
			author := firstNonEmpty(l.Author, defaultAuthor)
			row := map[string]interface{}{
				"platform": "INSTAGRAM", "url": l.URL, "shortcode": l.Shortcode, "type": l.Kind,
				"thumbnail_src": l.Thumbnail, "alt_text": l.Alt,
			}
			if author != "" {
				row["author"] = author
				row["author_url"] = profileURL(author)
			}
			out = append(out, row)
		}
		if len(out) >= maxCount {
			break
		}
		prev, stale = trackStagnation(len(out), prev, stale)
		if stale >= 3 {
			break
		}
		var ok bool
		_ = evalJS(p, `() => { window.scrollBy(0, Math.max(800, innerHeight)); window.dispatchEvent(new Event('scroll')); return true; }`, &ok)
		if err := pause(ctx, scrollSettle); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ListUserPosts returns up to maxCount posts from a profile's grid, each with
// url, shortcode, type, author, thumbnail_src and alt_text.
func (b *InstagramBot) ListUserPosts(ctx context.Context, p browser.PageInterface, target string, maxCount int) ([]map[string]interface{}, error) {
	if maxCount <= 0 {
		maxCount = 20
	}
	username, err := b.openProfile(ctx, p, target)
	if err != nil {
		return nil, err
	}
	if _, err := waitProfileHeader(ctx, p); err != nil {
		return nil, fmt.Errorf("instagram: %s's profile did not render: %w", username, err)
	}
	var st struct {
		Private bool `json:"private"`
		Posts   int  `json:"posts"`
	}
	_ = poll(ctx, findTimeout, func() (bool, error) {
		err := evalJS(p, `() => ({ private: /this account is private/i.test(VT()),
			posts: Array.from((document.querySelector('main') || document.body).querySelectorAll('a[href]')).filter((a) => !a.closest('header') && vis(a) && postRef(a.getAttribute('href'))).length })`, &st)
		return st.Private || st.Posts > 0, err
	})
	if st.Private && st.Posts == 0 {
		return nil, fmt.Errorf("instagram: %s is private — its posts are not visible", username)
	}
	return collectPostLinks(ctx, p, maxCount, username)
}

var tagRe = regexp.MustCompile(`^[\p{L}\p{N}_]+$`)

// SearchPosts returns up to maxCount posts for a keyword: the hashtag page
// for a single-word keyword, else (or when that page is empty) Instagram's
// keyword search.
func (b *InstagramBot) SearchPosts(ctx context.Context, p browser.PageInterface, keyword string, maxCount int) ([]map[string]interface{}, error) {
	kw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(keyword), "#"))
	if kw == "" {
		return nil, errors.New("instagram: keyword is required")
	}
	if maxCount <= 0 {
		maxCount = 20
	}
	var pages []string
	if tagRe.MatchString(kw) {
		pages = append(pages, b.SearchURL(kw))
	}
	pages = append(pages, "https://www.instagram.com/explore/search/keyword/?q="+url.QueryEscape(keyword))
	var lastErr error
	for _, u := range pages {
		if err := b.open(ctx, p, u); err != nil {
			if errors.Is(err, ErrNotLoggedIn) {
				return nil, err
			}
			lastErr = err
			continue
		}
		_ = poll(ctx, findTimeout, func() (bool, error) {
			var n int
			err := evalJS(p, `() => Array.from(document.querySelectorAll('main a[href], a[href]')).filter((a) => postRef(a.getAttribute('href'))).length`, &n)
			return n > 0, err
		})
		posts, err := collectPostLinks(ctx, p, maxCount, "")
		if err != nil {
			lastErr = err
			continue
		}
		if len(posts) > 0 {
			for _, row := range posts {
				row["keyword"] = keyword
			}
			return posts, nil
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("instagram: search %q: %w", keyword, lastErr)
	}
	return []map[string]interface{}{}, nil
}

// FindPostAuthors searches keyword and returns the profiles of up to
// maxCount distinct post authors (each with the post it was found through).
// A post whose author cannot be read is skipped; a profile that cannot be
// read is returned with just its username, url and profile_error.
func (b *InstagramBot) FindPostAuthors(ctx context.Context, p browser.PageInterface, keyword string, maxCount int) ([]map[string]interface{}, error) {
	if maxCount <= 0 {
		maxCount = 20
	}
	posts, err := b.SearchPosts(ctx, p, keyword, min(maxCount*3, 150))
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, post := range posts {
		if len(out) >= maxCount {
			break
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		postURL := getString(post, "url")
		author := getString(post, "author")
		if author == "" {
			data, err := b.ScrapePostData(ctx, p, postURL, 0)
			if err != nil {
				continue
			}
			author = getString(data, "author_username")
		}
		if author == "" || seen[strings.ToLower(author)] {
			continue
		}
		seen[strings.ToLower(author)] = true
		prof, err := b.GetUserInfo(ctx, p, author)
		if err != nil {
			if errors.Is(err, ErrNotLoggedIn) {
				return out, err
			}
			prof = map[string]interface{}{"platform": "INSTAGRAM", "username": author, "url": profileURL(author), "profile_error": err.Error()}
		}
		prof["source_post"] = postURL
		prof["keyword"] = keyword
		out = append(out, prof)
	}
	return out, nil
}
