//go:build !nosocial

package instagram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Follow / unfollow (WRITE)
// ---------------------------------------------------------------------------

// openProfile resolves a username or profile URL and opens the profile.
func (b *InstagramBot) openProfile(ctx context.Context, p browser.PageInterface, target string) (string, error) {
	username := b.usernameFrom(target)
	if username == "" {
		return "", fmt.Errorf("instagram: could not determine a username from %q", target)
	}
	if err := b.open(ctx, p, profileURL(username)); err != nil {
		return "", err
	}
	return username, nil
}

// waitFollowState waits for the profile header to render its follow button
// and returns followState(): following | requested | not_following, or an
// error when the header never shows one.
func waitFollowState(ctx context.Context, p browser.PageInterface) (string, error) {
	var state string
	err := poll(ctx, findTimeout, func() (bool, error) {
		var unavailable bool
		if err := evalJS(p, `() => followState()`, &state); err != nil {
			return false, err
		}
		if err := evalJS(p, `() => /sorry, this page isn.t available/i.test(VT())`, &unavailable); err == nil && unavailable {
			return false, stopErr{errors.New("instagram: this profile is not available")}
		}
		return state == "following" || state == "requested" || state == "not_following", nil
	})
	return state, err
}

// FollowUser follows the profile's owner. A profile already followed (or
// with a pending request) is reported as such and left alone; the click is
// verified by the header switching to Following / Requested. Only the
// profile header's own Follow button is ever clicked — never one in
// "Suggested for you".
func (b *InstagramBot) FollowUser(ctx context.Context, p browser.PageInterface, target string) (map[string]interface{}, error) {
	username, err := b.openProfile(ctx, p, target)
	if err != nil {
		return nil, err
	}
	state, err := waitFollowState(ctx, p)
	m := newMark()
	switch {
	case err == nil && (state == "following" || state == "requested"):
		return ok("url", profileURL(username), "username", username, "status", "already_"+state), nil
	case err == nil:
		var marked bool
		if err := evalJS(p, `(m) => { const b = profileButton(/^follow( back)?$/i); if (b) mark(b, m); return !!b; }`, &marked, m); err != nil || !marked {
			return nil, fmt.Errorf("instagram: Follow button vanished on %s", username)
		}
	case !errors.Is(err, errTimeout):
		return nil, err
	default:
		// No recognisable button: Jev may find it; accept only a real
		// Follow button, outside any list (suggestions) or dialog.
		d, jerr := b.jevPick(ctx, p, "click",
			fmt.Sprintf("the Follow button in the profile header of @%s (not a Follow button under 'Suggested for you')", username),
			"its text is exactly 'Follow' or 'Follow Back'; 'Following' or 'Requested' means already followed", m)
		if jerr != nil {
			return nil, fmt.Errorf("instagram: no Follow button on %s's profile: %v (jev: %v)", username, err, jerr)
		}
		switch {
		case followingRe.MatchString(d.Text):
			return ok("url", profileURL(username), "username", username, "status", "already_"+strings.ToLower(d.Text)), nil
		case !followRe.MatchString(d.Text) || d.InList || d.InDialog:
			return nil, fmt.Errorf("instagram: no Follow button on %s's profile (jev picked %q)", username, d.Text)
		}
	}
	if err := clickMarked(p, m); err != nil {
		return nil, fmt.Errorf("instagram: click Follow: %w", err)
	}
	var after string
	err = poll(ctx, verifyTimeout, func() (bool, error) {
		if err := evalJS(p, `(m) => {
			const s = followState();
			if (s === 'following' || s === 'requested') return s;
			const el = document.querySelector('['+MARK+'="'+m+'"]');
			const t = el ? T(el).toLowerCase() : '';
			return t === 'following' || t === 'requested' ? t : s;
		}`, &after, m); err != nil {
			return false, err
		}
		return after == "following" || after == "requested", nil
	})
	if err != nil {
		return nil, fmt.Errorf("instagram: follow of %s not confirmed (header still shows %q)", username, after)
	}
	status := "followed"
	if after == "requested" {
		status = "requested"
	}
	return ok("url", profileURL(username), "username", username, "status", status), nil
}

// UnfollowUser unfollows the profile's owner (or withdraws a pending
// request): header Following/Requested → the dialog's Unfollow → verified by
// the header showing Follow again. A profile not followed is left alone.
func (b *InstagramBot) UnfollowUser(ctx context.Context, p browser.PageInterface, target string) (map[string]interface{}, error) {
	username, err := b.openProfile(ctx, p, target)
	if err != nil {
		return nil, err
	}
	state, err := waitFollowState(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("instagram: no follow button on %s's profile: %w", username, err)
	}
	if state == "not_following" {
		return ok("url", profileURL(username), "username", username, "status", "not_following"), nil
	}
	m := newMark()
	var marked bool
	if err := evalJS(p, `(m) => { const b = profileButton(/^(following|requested)$/i); if (b) mark(b, m); return !!b; }`, &marked, m); err != nil || !marked {
		return nil, fmt.Errorf("instagram: Following button vanished on %s", username)
	}
	if err := clickMarked(p, m); err != nil {
		return nil, fmt.Errorf("instagram: click Following: %w", err)
	}
	// The dialog lists Close friends / Favorites / Mute / Restrict / Unfollow.
	c := newMark()
	err = poll(ctx, findTimeout, func() (bool, error) {
		var found bool
		err := evalJS(p, `(m) => {
			for (const d of dialogs()) {
				const b = byText(d, /^unfollow$/i)[0];
				if (b) { mark(b, m); return true; }
			}
			return false;
		}`, &found, c)
		return found, err
	})
	if err != nil {
		d, jerr := b.jevPick(ctx, p, "click",
			fmt.Sprintf("the Unfollow option in the dialog that opened after clicking Following on @%s's profile", username), "", c)
		if jerr != nil || !unfollowRe.MatchString(d.Text) {
			return nil, fmt.Errorf("instagram: Unfollow confirmation not found for %s: %v (jev: %v)", username, err, jerr)
		}
	}
	if err := pause(ctx, uiPause/2); err != nil {
		return nil, err
	}
	if err := clickMarked(p, c); err != nil {
		return nil, fmt.Errorf("instagram: click Unfollow: %w", err)
	}
	var after string
	err = poll(ctx, verifyTimeout, func() (bool, error) {
		if err := evalJS(p, `() => followState()`, &after); err != nil {
			return false, err
		}
		return after == "not_following", nil
	})
	if err != nil {
		return nil, fmt.Errorf("instagram: unfollow of %s not confirmed (header shows %q)", username, after)
	}
	return ok("url", profileURL(username), "username", username, "status", "unfollowed"), nil
}

// ---------------------------------------------------------------------------
// Stories (WRITE: viewing adds the account to the story's viewer list)
// ---------------------------------------------------------------------------

// ViewStories opens the profile's active story from the avatar ring (never a
// highlight) and watches up to maxStories of them (0 = all, capped at 50),
// stopping when the viewer moves on to another account. A profile without an
// active story is reported with stories_viewed 0.
func (b *InstagramBot) ViewStories(ctx context.Context, p browser.PageInterface, target string, maxStories int) (map[string]interface{}, error) {
	username, err := b.openProfile(ctx, p, target)
	if err != nil {
		return nil, err
	}
	if maxStories <= 0 || maxStories > 50 {
		maxStories = 50
	}
	if _, err := waitProfileHeader(ctx, p); err != nil {
		return nil, fmt.Errorf("instagram: %s's profile did not render: %w", username, err)
	}
	m := newMark()
	var ring string
	if err := evalJS(p, `(m) => {
		const h = hdr();
		// The avatar is in the header's first section; highlights (also
		// canvas rings) link to /stories/highlights/ and are never used.
		const cands = Array.from(h.querySelectorAll('[aria-label="View profile story"], [aria-label$="profile story"]'));
		const first = h.querySelector('section');
		if (first) for (const c of first.querySelectorAll('canvas')) { const b = c.closest('[role="button"], a'); if (b) cands.push(b); }
		const ok = cands.find((b) => !b.closest('a[href*="/highlights/"]') && !/highlight/i.test(lbl(b)));
		if (!ok) return 'none';
		mark(ok, m);
		return 'marked';
	}`, &ring, m); err != nil {
		return nil, err
	}
	if ring != "marked" {
		return ok("url", profileURL(username), "username", username, "stories_viewed", 0, "status", "no_active_story"), nil
	}
	if err := clickMarked(p, m); err != nil {
		return nil, fmt.Errorf("instagram: open story: %w", err)
	}
	// Some sessions get a "View as <you>?" interstitial first.
	viewerOpen := func() (bool, error) {
		var st struct {
			Open  bool   `json:"open"`
			Owner string `json:"owner"`
		}
		if err := evalJS(p, `() => {
			for (const d of dialogs().concat([document.body])) {
				const v = byText(d, /^view story$/i)[0];
				if (v && /view as/i.test(T(d))) { v.click(); return { open: false }; }
			}
			const m = location.pathname.match(/^\/stories\/([^/]+)\//);
			return { open: !!m, owner: m ? m[1] : '' };
		}`, &st); err != nil {
			return false, err
		}
		return st.Open && strings.EqualFold(st.Owner, username), nil
	}
	if err := poll(ctx, findTimeout, viewerOpen); err != nil {
		return nil, fmt.Errorf("instagram: story viewer did not open for %s: %w", username, err)
	}
	viewed := 0
	for viewed < maxStories {
		viewed++
		if err := pause(ctx, storyDwell); err != nil {
			return nil, err
		}
		if viewed >= maxStories {
			break
		}
		var before string
		_ = evalJS(p, `() => location.pathname`, &before)
		n := newMark()
		var hasNext bool
		_ = evalJS(p, `(m) => { const b = document.querySelector('button[aria-label="Next"], [role="button"][aria-label="Next"]'); if (b) mark(b, m); return !!b; }`, &hasNext, n)
		if hasNext {
			_ = clickMarked(p, n)
		} else if err := pressArrowRight(p); err != nil {
			break
		}
		// Wait for the next story (path changes) or the viewer leaving.
		var still bool
		_ = poll(ctx, 3*time.Second, func() (bool, error) {
			var path string
			if err := evalJS(p, `() => location.pathname`, &path); err != nil {
				return false, err
			}
			return path != before, nil
		})
		if s, err := viewerOpen(); err == nil {
			still = s
		}
		if !still {
			break
		}
	}
	// Close the viewer if it is still on this account's stories.
	if s, _ := viewerOpen(); s {
		c := newMark()
		var hasClose bool
		_ = evalJS(p, `(m) => { const s = document.querySelector('svg[aria-label="Close"]'); const b = s && btnOf(s); if (b) mark(b, m); return !!b; }`, &hasClose, c)
		if hasClose {
			_ = clickMarked(p, c)
		}
	}
	return ok("url", profileURL(username), "username", username, "stories_viewed", viewed, "status", "viewed"), nil
}

// waitProfileHeader waits for the profile header.
func waitProfileHeader(ctx context.Context, p browser.PageInterface) (bool, error) {
	err := poll(ctx, findTimeout, func() (bool, error) {
		var has bool
		if err := evalJS(p, `() => !!hdr()`, &has); err != nil {
			return false, err
		}
		return has, nil
	})
	return err == nil, err
}
