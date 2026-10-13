//go:build !nosocial

package instagram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Profile data (READ)
// ---------------------------------------------------------------------------

// GetUserInfo returns profile data for username. It opens the profile and
// reads what the page itself loaded for it (the profile's record in the
// page's data store: bio links, pronouns, category, contact details,
// highlights…); when that isn't there it asks Instagram's web_profile_info
// endpoint from inside the page (same origin, the user's own session —
// exactly what Instagram's frontend does), and last parses the rendered
// header.
func (b *InstagramBot) GetUserInfo(ctx context.Context, p browser.PageInterface, username string) (map[string]interface{}, error) {
	if username == "" {
		return nil, fmt.Errorf("instagram: username is required for get_user_info")
	}
	if cur, _ := p.GetURL(); !strings.EqualFold(b.ExtractUsername(cur), username) || !strings.Contains(hostOf(cur), "instagram.com") {
		if err := b.open(ctx, p, profileURL(username)); err != nil {
			return nil, err
		}
	}
	if res := b.profileFromStore(ctx, p, username); res != nil {
		return res, nil
	}
	if res, err := b.fetchProfileViaJS(p, username); err == nil {
		return res, nil
	}
	var res map[string]interface{}
	err := poll(ctx, findTimeout, func() (bool, error) {
		r, err := b.profileFromDOM(p, username)
		if err != nil {
			return false, err
		}
		res = r
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("instagram: could not read profile of %s: %w", username, err)
	}
	return res, nil
}

// storeProfileScript finds the profile's user record in the page's Relay
// store (reached through the React tree) with its linked records resolved
// one level, and the titles of its story highlights. state is "ok" with
// the record, "header" when the header rendered without one (the store
// isn't reachable, or holds no such user), else "wait".
const storeProfileScript = `(username) => {
	const h = hdr();
	const none = { state: h ? 'header' : 'wait' };
	let env = null;
	for (const el of [h, document.querySelector('main'), document.body]) {
		if (!el || env) continue;
		const fk = Object.keys(el).find((k) => k.startsWith('__reactFiber$'));
		for (let f = fk ? el[fk] : null, i = 0; f && i < 300 && !env; f = f.return, i++) {
			const p = f.memoizedProps;
			if (p && p.environment && typeof p.environment.getStore === 'function') env = p.environment;
		}
	}
	if (!env) return none;
	let src;
	try { src = env.getStore().getSource(); } catch (e) { return none; }
	const want = String(username).toLowerCase();
	let rec = null;
	for (const id of src.getRecordIDs()) {
		const r = src.get(id);
		if (!r || r.__typename !== 'XDTUserDict' || typeof r.username !== 'string' || r.username.toLowerCase() !== want) continue;
		if (!('biography' in r) || !('follower_count' in r)) continue;
		if (!rec || Object.keys(r).length > Object.keys(rec).length) rec = r;
	}
	if (!rec) return none;
	const deref = (v) => {
		if (v && typeof v === 'object' && v.__ref) return src.get(v.__ref) || null;
		if (v && typeof v === 'object' && Array.isArray(v.__refs)) return v.__refs.map((r) => src.get(r)).filter(Boolean);
		return v;
	};
	const user = {};
	for (const k of Object.keys(rec)) {
		if (k.startsWith('__') || k.includes('(')) continue;
		const v = deref(rec[k]);
		if (typeof v === 'function') continue;
		user[k] = v;
	}
	// Story highlights, in the tray's order when the tray connection is
	// there, else as the store holds them.
	const titles = [];
	const reelTitle = (r) => { if (r && r.__typename === 'XDTReelDict' && typeof r.title === 'string' && r.title) titles.push(r.title); };
	const root = src.get('client:root') || {};
	const trayKey = Object.keys(root).find((k) => k.includes('highlights_tray_connection') && k.includes('"' + rec.pk + '"'));
	const tray = trayKey ? deref(root[trayKey]) : null;
	const edges = tray ? deref(tray.edges) : null;
	if (Array.isArray(edges)) {
		for (const e of edges) reelTitle(deref(e && e.node));
	} else {
		for (const id of src.getRecordIDs()) {
			const r = src.get(id);
			if (r && r.__typename === 'XDTReelDict' && String(r.id || '').startsWith('highlight:') && r.user && r.user.__ref === rec.__id) reelTitle(r);
		}
	}
	return { state: 'ok', user: JSON.parse(JSON.stringify(user)), highlights: titles, tray: Array.isArray(edges) };
}`

// profileFromStore reads the profile from the page's data store, or
// returns nil when the page doesn't hold it.
func (b *InstagramBot) profileFromStore(ctx context.Context, p browser.PageInterface, username string) map[string]interface{} {
	var out struct {
		State      string                 `json:"state"`
		User       map[string]interface{} `json:"user"`
		Highlights []string               `json:"highlights"`
		Tray       bool                   `json:"tray"`
	}
	headerSeen, trayWait := 0, 0
	_ = poll(ctx, findTimeout, func() (bool, error) {
		if err := evalJS(p, storeProfileScript, &out, username); err != nil {
			return false, err
		}
		switch out.State {
		case "ok":
			// The highlights tray is a query of its own that can resolve
			// after the profile's: give it a moment.
			trayWait++
			return out.Tray || len(out.Highlights) > 0 || trayWait >= 6, nil
		case "header":
			// The record arrives shortly after the header; don't wait for
			// one the page will never hold.
			headerSeen++
			return headerSeen >= 6, nil
		}
		return false, nil
	})
	if out.State != "ok" || out.User == nil {
		return nil
	}
	res := buildProfileResult(username, out.User)
	if len(out.Highlights) > 0 {
		res["highlights"] = out.Highlights
	}
	return res
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// fetchProfileViaJS calls /api/v1/users/web_profile_info/ from the page.
func (b *InstagramBot) fetchProfileViaJS(p browser.PageInterface, username string) (map[string]interface{}, error) {
	var out struct {
		Error string                 `json:"error"`
		Data  map[string]interface{} `json:"data"`
	}
	err := evalJS(p, `async (u) => {
		const csrf = (document.cookie.match(/csrftoken=([^;]+)/) || [])[1] || '';
		try {
			const r = await fetch('/api/v1/users/web_profile_info/?username=' + encodeURIComponent(u), {
				headers: { 'x-ig-app-id': '936619743392459', 'x-requested-with': 'XMLHttpRequest', 'x-csrftoken': csrf },
				credentials: 'include',
			});
			if (!r.ok) return { error: 'HTTP ' + r.status };
			return { data: await r.json() };
		} catch (e) { return { error: String((e && e.message) || e) }; }
	}`, &out, username)
	if err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("web_profile_info: %s", out.Error)
	}
	user := extractUserFromResponse(out.Data)
	if user == nil {
		return nil, errors.New("web_profile_info: no user in response")
	}
	if u := getString(user, "username"); u != "" && !strings.EqualFold(u, username) {
		return nil, fmt.Errorf("web_profile_info: asked for %q, got %q", username, u)
	}
	return buildProfileResult(username, user), nil
}

// extractUserFromResponse navigates the JSON response structure to find the
// user object. Instagram uses different response formats:
//   - web_profile_info: {"data": {"user": {...}}}
//   - graphql: {"data": {"user": {...}}} or {"graphql": {"user": {...}}}
func extractUserFromResponse(raw map[string]interface{}) map[string]interface{} {
	if data, ok := raw["data"].(map[string]interface{}); ok {
		if user, ok := data["user"].(map[string]interface{}); ok {
			if _, hasID := user["id"]; hasID {
				return user
			}
			if _, hasUsername := user["username"]; hasUsername {
				return user
			}
		}
	}
	if gql, ok := raw["graphql"].(map[string]interface{}); ok {
		if user, ok := gql["user"].(map[string]interface{}); ok {
			return user
		}
	}
	return nil
}

// buildProfileResult converts an Instagram user object to our standard
// format. It reads both shapes Instagram serves: the page store's record
// (follower_count, bio_links, hd_profile_pic_url_info…) and the
// web_profile_info response (edge_followed_by, profile_pic_url_hd,
// business_email…). The bio is "bio": it is the person's About, never an
// introduction.
func buildProfileResult(username string, user map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"platform":    "INSTAGRAM",
		"username":    username,
		"url":         profileURL(username),
		"full_name":   getString(user, "full_name"),
		"bio":         getString(user, "biography"),
		"is_verified": getBool(user, "is_verified"),
		"is_private":  getBool(user, "is_private"),
		"website":     getString(user, "external_url"),
	}
	hd := ""
	if info, ok := user["hd_profile_pic_url_info"].(map[string]interface{}); ok {
		hd = getString(info, "url")
	}
	result["image_url"] = firstNonEmpty(hd, getString(user, "profile_pic_url_hd"), getString(user, "profile_pic_url"))
	count := func(flat, edge string) (string, bool) {
		if c, ok := user[flat].(float64); ok {
			return strconv.FormatInt(int64(c), 10), true
		}
		if e, ok := user[edge].(map[string]interface{}); ok {
			if c, ok := e["count"].(float64); ok {
				return strconv.FormatInt(int64(c), 10), true
			}
		}
		return "", false
	}
	if c, ok := count("follower_count", "edge_followed_by"); ok {
		result["follower_count"] = c
	}
	if c, ok := count("following_count", "edge_follow"); ok {
		result["following_count"] = c
	}
	if c, ok := count("media_count", "edge_owner_to_timeline_media"); ok {
		result["content_count"] = c
	}
	if id := firstNonEmpty(getString(user, "pk"), getString(user, "id")); id != "" {
		result["platform_id"] = id
	}
	// The category, unless the account hides it from its profile.
	if show, ok := user["should_show_category"].(bool); !ok || show {
		if cat := firstNonEmpty(getString(user, "category"), getString(user, "category_name"), getString(user, "business_category_name")); cat != "" {
			result["profile_category"] = cat
		}
	}
	if t := accountType(user); t != "" {
		result["account_type"] = t
	}
	if pr := stringList(user["pronouns"]); len(pr) > 0 {
		result["pronouns"] = pr
	}
	if links := bioLinks(user); len(links) > 0 {
		result["links"] = links
		if result["website"] == "" {
			result["website"] = links[0]["url"]
		}
	}
	if getBool(user, "show_text_post_app_badge") {
		result["threads_handle"] = firstNonEmpty(getString(user, "text_post_app_badge_label"), username)
	}
	if c := contactOf(user); len(c) > 0 {
		result["contact"] = c
	}
	return result
}

// accountType names the account's kind: Instagram's account_type 1/2/3, or
// the web_profile_info flags.
func accountType(user map[string]interface{}) string {
	switch t, _ := user["account_type"].(float64); t {
	case 1:
		return "personal"
	case 2:
		return "business"
	case 3:
		return "creator"
	}
	switch {
	case getBool(user, "is_business_account") || getBool(user, "is_business"):
		return "business"
	case getBool(user, "is_professional_account"):
		return "creator"
	}
	return ""
}

// bioLinks lists the profile's link-in-bio entries ({url, title}), with
// Instagram's l.instagram.com redirect unwrapped.
func bioLinks(user map[string]interface{}) []map[string]interface{} {
	raw, _ := user["bio_links"].([]interface{})
	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, r := range raw {
		l, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		u := firstNonEmpty(getString(l, "url"), unwrapLinkShim(getString(l, "lynx_url")))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		link := map[string]interface{}{"url": u}
		if t := getString(l, "title"); t != "" {
			link["title"] = t
		}
		out = append(out, link)
	}
	return out
}

// contactOf returns the contact details a business profile shows
// publicly ({email, phone, address}).
func contactOf(user map[string]interface{}) map[string]interface{} {
	c := map[string]interface{}{}
	if e := firstNonEmpty(getString(user, "public_email"), getString(user, "business_email")); e != "" {
		c["email"] = e
	}
	phone := getString(user, "public_phone_number")
	if phone != "" {
		if cc := getString(user, "public_phone_country_code"); cc != "" && !strings.HasPrefix(phone, "+") {
			phone = "+" + cc + " " + phone
		}
	}
	if phone = firstNonEmpty(phone, getString(user, "contact_phone_number"), getString(user, "business_phone_number")); phone != "" {
		c["phone"] = phone
	}
	var addr []string
	for _, k := range []string{"address_street", "city_name", "zip"} {
		if v := getString(user, k); v != "" {
			addr = append(addr, v)
		}
	}
	if len(addr) == 0 {
		// web_profile_info: a JSON string {street_address, zip_code, city_name}.
		var a map[string]interface{}
		if jsonUnmarshal([]byte(getString(user, "business_address_json")), &a) == nil {
			for _, k := range []string{"street_address", "city_name", "zip_code"} {
				if v := getString(a, k); v != "" {
					addr = append(addr, v)
				}
			}
		}
	}
	if len(addr) > 0 {
		c["address"] = strings.Join(addr, ", ")
	}
	return c
}

// stringList returns the non-empty strings of a JSON list.
func stringList(v interface{}) []string {
	l, _ := v.([]interface{})
	var out []string
	for _, e := range l {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
