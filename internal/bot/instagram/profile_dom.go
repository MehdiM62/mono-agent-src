//go:build !nosocial

package instagram

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// domProfile is what profileScript reads off a rendered profile page.
type domProfile struct {
	State       string   `json:"state"`
	Username    string   `json:"username"`
	FullName    string   `json:"full_name"`
	Bio         string   `json:"bio"`
	Followers   string   `json:"followers"`
	Following   string   `json:"following"`
	Posts       string   `json:"posts"`
	Image       string   `json:"image"`
	Website     string   `json:"website"`
	Verified    bool     `json:"verified"`
	Private     bool     `json:"private"`
	OGDesc      string   `json:"og_description"`
	OGTitle     string   `json:"og_title"`
	LDJSON      []string `json:"ldjson"`
	Category    string   `json:"category"`
	Unavailable bool     `json:"unavailable"`
}

const profileScript = `() => {
	const meta = (p) => { const m = document.querySelector('meta[property="' + p + '"]'); return m ? m.getAttribute('content') || '' : ''; };
	const out = { og_description: meta('og:description'), og_title: meta('og:title'),
		ldjson: Array.from(document.querySelectorAll('script[type="application/ld+json"]')).map((s) => s.textContent) };
	const body = VT();
	out.unavailable = /sorry, this page isn.t available/i.test(body);
	out.private = /this account is private/i.test(body);
	const h = hdr();
	if (!h) { out.state = 'no_header'; return out; }
	out.state = 'ok';
	const h2 = h.querySelector('h2, h1');
	out.username = h2 ? T(h2) : '';
	out.verified = !!h.querySelector('svg[aria-label="Verified"], [title="Verified"]');
	const img = h.querySelector('img[alt*="profile picture"]') || h.querySelector('img');
	out.image = img ? img.getAttribute('src') || '' : '';
	// Counts: read off the rendered header, whatever its layout. The page's
	// og:description is only a fallback — Instagram serves it stale (the
	// header of your own profile can say 674 following while og: says 706).
	const st = headerStats(h);
	if (st.posts) out.posts = st.posts.num;
	if (st.followers) out.followers = st.followers.num;
	if (st.following) out.following = st.following.num;
	const statEls = Object.values(st).map((x) => x.el);
	const isStat = (el) => statEls.some((x) => x.contains(el) || el.contains(x));
	const skip = (el) => !!el.closest('[role="menu"]') || isStat(el);
	// Website: an outbound link (never the Threads badge next to the name),
	// else the text of the link button ("example.com and 2 more").
	for (const a of h.querySelectorAll('a[href]')) {
		const href = a.getAttribute('href') || '';
		if (skip(a) || userOf(href) || isThreadsBadge(a)) continue;
		const d = linkDest(href);
		if (!d || !/^https?:$/.test(d.protocol)) continue;
		if (a.target === '_blank' || /(^|\.)l\.instagram\.com$/i.test(new URL(href, location.href).hostname) || !/(^|\.)instagram\.com$/i.test(d.hostname)) { out.website = href; break; }
	}
	if (!out.website) {
		const icon = h.querySelector('svg[aria-label="Link icon"]');
		const b = icon && icon.closest('button, a, [role="button"], [role="link"]');
		const t = b ? T(b).replace(/\s+and \d+ more$/i, '') : '';
		if (t) out.website = t;
	}
	// Name: the first plain text line of the header that is not the username,
	// a count, a button or a link. Bio: the (expandable) text behind a
	// role=button, else the next plain line.
	const lines = Array.from(h.querySelectorAll('span[dir="auto"]')).filter((x) => !skip(x) &&
		!x.closest('a, button, [role="button"], [role="link"], h1, h2') && !x.querySelector('span[dir="auto"]'));
	const texts = lines.map(T).filter((t) => t && t !== out.username && !/^[•·]$/.test(t));
	if (texts.length) out.full_name = texts[0];
	const bioBtn = Array.from(h.querySelectorAll('[role="button"] span[dir="auto"]')).find((x) => !skip(x) &&
		!x.closest('button') && !/^(more|less|\.\.\.)$/i.test(T(x)) && T(x) !== out.full_name && !x.parentElement.closest('[role="button"] span[dir="auto"]'));
	out.bio = bioBtn ? T(bioBtn) : (texts[1] || '');
	const cat = h.querySelector('[data-category], div[class*="category"]');
	if (cat) out.category = T(cat);
	return out;
}`

var (
	listKindRe  = regexp.MustCompile(`(?i)^(followers?|following)(_fetch)?$`)
	followRe    = regexp.MustCompile(`(?i)^follow( back)?$`)
	followingRe = regexp.MustCompile(`(?i)^(following|requested)$`)
	unfollowRe  = regexp.MustCompile(`(?i)^unfollow$`)
)

var ogCountsRe = regexp.MustCompile(`(?i)([\d.,]+\s*[kmb]?)\s+followers?,\s*([\d.,]+\s*[kmb]?)\s+following,\s*([\d.,]+\s*[kmb]?)\s+posts?`)
var ogTitleRe = regexp.MustCompile(`^(.*?)\s*\(@([A-Za-z0-9._]+)\)`)

// profileFromDOM parses the rendered profile page for username.
func (b *InstagramBot) profileFromDOM(p browser.PageInterface, username string) (map[string]interface{}, error) {
	var d domProfile
	if err := evalJS(p, profileScript, &d); err != nil {
		return nil, err
	}
	if d.Unavailable {
		return nil, stopErr{fmt.Errorf("instagram: profile %q is not available", username)}
	}
	if d.State != "ok" && d.OGDesc == "" {
		return nil, fmt.Errorf("profile header not rendered (%s)", d.State)
	}
	if username == "" {
		username = d.Username
	}
	res := map[string]interface{}{
		"platform":    "INSTAGRAM",
		"username":    username,
		"url":         profileURL(username),
		"full_name":   d.FullName,
		"bio":         d.Bio,
		"is_verified": d.Verified,
		"is_private":  d.Private,
		"image_url":   d.Image,
		"website":     unwrapLinkShim(d.Website),
	}
	if d.Category != "" {
		res["profile_category"] = d.Category
	}
	followers, following, posts := d.Followers, d.Following, d.Posts
	if m := ogCountsRe.FindStringSubmatch(d.OGDesc); m != nil {
		followers, following, posts = firstNonEmpty(followers, m[1]), firstNonEmpty(following, m[2]), firstNonEmpty(posts, m[3])
	}
	if m := ogTitleRe.FindStringSubmatch(d.OGTitle); m != nil && d.FullName == "" {
		res["full_name"] = strings.TrimSpace(m[1])
	}
	for _, s := range d.LDJSON {
		var ld map[string]interface{}
		if jsonUnmarshal([]byte(s), &ld) != nil {
			continue
		}
		if name := getString(ld, "name"); name != "" && res["full_name"] == "" {
			res["full_name"] = name
		}
		if desc := getString(ld, "description"); desc != "" && res["bio"] == "" {
			res["bio"] = desc
		}
	}
	if c := parseCount(followers); c != "" {
		res["follower_count"] = c
	}
	if c := parseCount(following); c != "" {
		res["following_count"] = c
	}
	if c := parseCount(posts); c != "" {
		res["content_count"] = c
	}
	return res, nil
}

// unwrapLinkShim turns Instagram's l.instagram.com/?u=<url> redirect into
// the destination URL.
func unwrapLinkShim(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Hostname(), "l.instagram.com") {
		return link
	}
	if dest := u.Query().Get("u"); dest != "" {
		return dest
	}
	return link
}

// parseCount normalises "1,234" → "1234" and "12.5K" / "686m" → an integer
// string; anything unparseable is returned trimmed.
func parseCount(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, strings.TrimSpace(strings.TrimSuffix(s, "k"))
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, strings.TrimSpace(strings.TrimSuffix(s, "m"))
	case strings.HasSuffix(s, "b"):
		mult, s = 1e9, strings.TrimSpace(strings.TrimSuffix(s, "b"))
	}
	if mult == 1 {
		digits := strings.NewReplacer(",", "", ".", "", " ", "").Replace(s)
		if _, err := strconv.ParseInt(digits, 10, 64); err == nil {
			return digits
		}
		return s
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return s
	}
	return strconv.FormatInt(int64(f*mult+0.5), 10)
}

// ExtractUsernameFromMetadata returns the username of the profile — or the
// author of the post — shown on the current page, from its JSON-LD, Open
// Graph, canonical link or post header.
func (b *InstagramBot) ExtractUsernameFromMetadata(ctx context.Context, p browser.PageInterface) (string, error) {
	var u string
	err := poll(ctx, 3*time.Second, func() (bool, error) {
		if err := evalJS(p, `() => {
			for (const s of document.querySelectorAll('script[type="application/ld+json"]')) {
				try {
					const ld = JSON.parse(s.textContent);
					const items = Array.isArray(ld) ? ld : [ld];
					for (const it of items) {
						const alt = it.alternateName || (it.author && it.author.alternateName) || '';
						if (typeof alt === 'string' && alt.startsWith('@')) return alt.slice(1);
					}
				} catch (e) {}
			}
			const urls = [];
			const og = document.querySelector('meta[property="og:url"]');
			if (og) urls.push(og.getAttribute('content') || '');
			const can = document.querySelector('link[rel="canonical"]');
			if (can) urls.push(can.getAttribute('href') || '');
			urls.push(location.href);
			for (const u of urls) {
				const ref = postRef(u);
				if (ref) { if (ref.author) return ref.author; continue; }
				const who = userOf(u);
				if (who) return who;
			}
			// A post page: the author link in the post header.
			for (const h of document.querySelectorAll('main header, article header')) {
				const a = Array.from(h.querySelectorAll('a[href]')).find((x) => userOf(x.getAttribute('href')));
				if (a) return userOf(a.getAttribute('href'));
			}
			const t = document.querySelector('meta[property="og:title"]');
			const m = t && (t.getAttribute('content') || '').match(/@([A-Za-z0-9._]{1,30})/);
			return m ? m[1] : '';
		}`, &u); err != nil {
			return false, err
		}
		return u != "", nil
	})
	if err != nil || u == "" {
		return "", fmt.Errorf("could not extract username from page metadata")
	}
	return u, nil
}
