//go:build !nosocial

package tiktok

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Profile data
// ---------------------------------------------------------------------------

const jsProfile = `
const txt = (sels) => { for (const s of sels) { const e = document.querySelector(s); if (e && norm(e.innerText || e.textContent)) return norm(e.innerText || e.textContent); } return ''; };
const titleEl = document.querySelector('[data-e2e="user-title"]');
const subEl = document.querySelector('[data-e2e="user-subtitle"]');
const title = txt(['[data-e2e="user-title"]']);
const subtitle = txt(['[data-e2e="user-subtitle"]']);
if (!title && !subtitle) return {found: false};
const img = document.querySelector('img[data-e2e="user-avatar"], [data-e2e="user-avatar"] img');
const link = document.querySelector('a[data-e2e="user-link"], [data-e2e="user-link"] a, [data-e2e="user-link"]');
// The verified badge carries no data-e2e or label on the current layout: it
// is an svg drawn as a #20D5EC circle next to the name/handle.
const idBox = (() => {
  const a = titleEl || subEl;
  for (let p = a && a.parentElement; p && p !== document.body; p = p.parentElement)
    if ((!titleEl || p.contains(titleEl)) && (!subEl || p.contains(subEl))) return p;
  return null;
})();
const badge = (root) => !!root && [...root.querySelectorAll('svg')].some(v =>
  [...v.querySelectorAll('circle,path,rect')].some(x => /^#?20d5ec$/i.test(x.getAttribute('fill') || '')) ||
  /verified/i.test((v.getAttribute('aria-label') || '') + ' ' + ((v.querySelector('title') || {}).textContent || '')));
return {
  found: true,
  title: title,
  subtitle: subtitle,
  bio: txt(['[data-e2e="user-bio"]']),
  following_count: txt(['[data-e2e="following-count"]']),
  follower_count: txt(['[data-e2e="followers-count"]']),
  likes_count: txt(['[data-e2e="likes-count"]']),
  profile_picture_url: img ? (img.currentSrc || img.src || '') : '',
  website: link ? (link.getAttribute('href') || norm(link.innerText)) : '',
  is_verified: !!document.querySelector('[data-e2e="verify-badge"], [data-e2e="user-verified"]') || badge(idBox),
};
`

// bioPlaceholder is what TikTok shows in user-bio when the account has no
// bio; it is not the account's bio.
var bioPlaceholder = regexp.MustCompile(`(?i)^no bio yet\.?$`)

// profileNames splits the profile header into (handle, display name). On
// tiktok.com user-title is the display name and user-subtitle the @handle.
// The handle in the URL is authoritative: whichever element spells it is the
// handle (the subtitle when both do, i.e. the display name equals the
// handle), the other one the display name.
func profileNames(urlHandle, title, subtitle string) (handle, fullName string) {
	same := func(a, b string) bool { return a != "" && normUser(a) == normUser(b) }
	switch {
	case urlHandle == "":
		return strings.TrimPrefix(subtitle, "@"), title
	case same(subtitle, urlHandle):
		return strings.TrimPrefix(subtitle, "@"), title
	case same(title, urlHandle):
		return strings.TrimPrefix(title, "@"), subtitle
	default:
		// Neither element spells the URL's handle (e.g. the account renamed
		// and TikTok redirected): the layout rule decides.
		return urlHandle, title
	}
}

// GetProfileData scrapes the currently loaded TikTok profile page.
func (b *TikTokBot) GetProfileData(ctx context.Context, page browser.PageInterface) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _, _ = page.Race([]string{"[data-e2e='user-title']", "[data-e2e='user-subtitle']"}, findTimeout)
	var data map[string]interface{}
	if err := evalJS(page, "", jsProfile, &data); err != nil {
		return nil, fmt.Errorf("tiktok: read profile: %w", err)
	}
	if found, _ := data["found"].(bool); !found {
		cur, _ := page.GetURL()
		return nil, fmt.Errorf("tiktok: no profile header on %s", cur)
	}
	delete(data, "found")
	cur, _ := page.GetURL()
	data["profile_url"] = cur
	title, _ := data["title"].(string)
	subtitle, _ := data["subtitle"].(string)
	delete(data, "title")
	delete(data, "subtitle")
	username := b.ExtractUsername(cur)
	handle, fullName := profileNames(username, title, subtitle)
	if handle == "" {
		handle = username
	}
	if username == "" {
		username = handle
	}
	data["handle"], data["full_name"], data["username"] = handle, fullName, username
	if bio, _ := data["bio"].(string); bioPlaceholder.MatchString(strings.TrimSpace(bio)) {
		data["bio"] = ""
	}
	if w, _ := data["website"].(string); w != "" {
		data["website"] = unwrapBioLink(w)
		data["links"] = []map[string]interface{}{{"url": data["website"]}}
	}
	// The page's own data has what the header doesn't show as text (exact
	// counts, video count, private flag, language) and is not subject to
	// the layout.
	var u universalProfile
	if err := evalJS(page, "", jsUniversalProfile, &u); err == nil && u.User.UniqueID != "" &&
		(username == "" || normUser(u.User.UniqueID) == normUser(username)) {
		u.apply(data)
	}
	return data, nil
}

// jsUniversalProfile returns the profile's user and stats from the JSON
// tiktok.com embeds in the page (__UNIVERSAL_DATA_FOR_REHYDRATION__), or
// null. After in-app navigation that JSON can still describe the page the
// tab first loaded; the caller checks it names this profile.
const jsUniversalProfile = `
const s = document.getElementById('__UNIVERSAL_DATA_FOR_REHYDRATION__');
if (!s) return null;
let d;
try { d = JSON.parse(s.textContent); } catch (e) { return null; }
const info = d && d.__DEFAULT_SCOPE__ && d.__DEFAULT_SCOPE__['webapp.user-detail'] && d.__DEFAULT_SCOPE__['webapp.user-detail'].userInfo;
if (!info || !info.user) return null;
return { user: info.user, stats: info.statsV2 || info.stats || {} };
`

// universalProfile is the part of the embedded profile JSON we read.
type universalProfile struct {
	User struct {
		ID             string `json:"id"`
		UniqueID       string `json:"uniqueId"`
		Nickname       string `json:"nickname"`
		Signature      string `json:"signature"`
		AvatarLarger   string `json:"avatarLarger"`
		AvatarMedium   string `json:"avatarMedium"`
		Verified       bool   `json:"verified"`
		PrivateAccount bool   `json:"privateAccount"`
		Language       string `json:"language"`
		IsOrganization int    `json:"isOrganization"`
		BioLink        *struct {
			Link string `json:"link"`
		} `json:"bioLink"`
		CommerceUserInfo *struct {
			CommerceUser bool   `json:"commerceUser"`
			Category     string `json:"category"`
		} `json:"commerceUserInfo"`
	} `json:"user"`
	// statsV2 holds exact counts as strings, stats rounded numbers.
	Stats map[string]interface{} `json:"stats"`
}

// apply overlays the embedded JSON onto the header read.
func (u universalProfile) apply(data map[string]interface{}) {
	set := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			data[k] = v
		}
	}
	set("username", u.User.UniqueID)
	set("handle", u.User.UniqueID)
	set("full_name", u.User.Nickname)
	if !bioPlaceholder.MatchString(strings.TrimSpace(u.User.Signature)) {
		set("bio", u.User.Signature)
	}
	set("profile_picture_url", firstNonEmpty(u.User.AvatarLarger, u.User.AvatarMedium))
	set("language", u.User.Language)
	set("platform_id", u.User.ID)
	data["is_verified"] = u.User.Verified || data["is_verified"] == true
	data["is_private"] = u.User.PrivateAccount
	if u.User.BioLink != nil && strings.TrimSpace(u.User.BioLink.Link) != "" {
		link := withScheme(u.User.BioLink.Link)
		data["website"] = link
		data["links"] = []map[string]interface{}{{"url": link}}
	}
	if c := u.User.CommerceUserInfo; c != nil {
		set("profile_category", c.Category)
	}
	switch {
	case u.User.IsOrganization == 1:
		data["account_type"] = "organization"
	case u.User.CommerceUserInfo != nil && u.User.CommerceUserInfo.CommerceUser:
		data["account_type"] = "business"
	}
	for key, stat := range map[string]string{
		"follower_count": "followerCount", "following_count": "followingCount", "likes_count": "heartCount",
		"content_count": "videoCount", "friend_count": "friendCount",
	} {
		switch v := u.Stats[stat].(type) {
		case string:
			set(key, v)
		case float64:
			data[key] = fmt.Sprintf("%.0f", v)
		}
	}
}

// withScheme makes a bio link ("linktr.ee/x") an absolute URL.
func withScheme(link string) string {
	link = strings.TrimSpace(link)
	if link == "" || strings.Contains(link, "://") {
		return link
	}
	return "https://" + link
}

// unwrapBioLink turns tiktok.com's /link/v2?target=<url> redirect into the
// destination.
func unwrapBioLink(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Hostname(), "tiktok.com") || !strings.HasPrefix(u.Path, "/link") {
		return link
	}
	if t := u.Query().Get("target"); t != "" {
		return withScheme(t)
	}
	return link
}

// getProfileData navigates to target (profile URL or handle) when given and
// scrapes it.
func (b *TikTokBot) getProfileData(ctx context.Context, page browser.PageInterface, target string) (map[string]interface{}, error) {
	if strings.TrimSpace(target) != "" {
		u, _, err := profileTarget(target)
		if err != nil {
			return nil, err
		}
		release, err := openQuiet(ctx, page, u)
		defer release()
		if err != nil {
			return nil, err
		}
	}
	return b.GetProfileData(ctx, page)
}
