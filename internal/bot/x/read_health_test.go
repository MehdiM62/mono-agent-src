//go:build !nosocial

package x

// Offline tests of the X read paths (profile, search, followers, inbox)
// driven by healthPage with canned page-script results.

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func healthIDs(rows []map[string]interface{}, key string) []string {
	var out []string
	for _, r := range rows {
		s, _ := r[key].(string)
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------------------
// get_profile_data
// ---------------------------------------------------------------------------

func healthProfilePage(t *testing.T, scenario string) *healthPage {
	p := newHealthPage("about:blank")
	p.on("ready", profileReadyJS, healthConst("profile"))
	p.on("scrape", scrapeProfileJS, healthConst(healthFixture(t, "health_profile.json", scenario)))
	p.on("pinned", pinnedTextJS, healthSeq("", "Pinned via timeline"))
	return p
}

func TestHealthGetProfileOverlaysUserObject(t *testing.T) {
	healthFast(t)
	p := healthProfilePage(t, "with_user")
	data, err := New().GetProfile(context.Background(), p, "@synth_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://x.com/synth_alice"}) {
		t.Errorf("navigated %v", p.navs)
	}
	want := map[string]interface{}{
		"profile_url":         "https://x.com/synth_alice",
		"username":            "synth_alice",
		"full_name":           "Alice Synth",
		"location":            "Berlin",
		"platform_id":         "424242",
		"bio":                 "Building things at https://work.example",
		"website":             "https://alice.example/home",
		"website_href":        "https://t.co/abc",
		"followers_count":     int64(1234),
		"following_count":     int64(3456),
		"content_count":       int64(12000345),
		"join_date":           "2006-03-21",
		"birth_date":          "June 5, 1990",
		"profile_category":    "Artist",
		"account_type":        "creator",
		"verification_type":   "blue",
		"is_verified":         true,
		"is_protected":        false,
		"profile_picture_url": "https://pbs.twimg.com/profile_images/1/a_400x400.jpg",
		"banner_url":          "https://pbs.twimg.com/profile_banners/1/1500x500",
		"affiliates_count":    int64(7),
		"followers_text":      "1.2K",
	}
	for k, v := range want {
		if !reflect.DeepEqual(data[k], v) {
			t.Errorf("%s = %#v, want %#v", k, data[k], v)
		}
	}
	wantLinks := []map[string]interface{}{
		{"url": "https://alice.example/home", "title": "alice.example/home"},
		{"url": "https://work.example", "title": "work.example"},
	}
	if !reflect.DeepEqual(data["links"], wantLinks) {
		t.Errorf("links = %#v", data["links"])
	}
	pin, _ := data["pinned_post"].(map[string]interface{})
	if pin["url"] != "https://x.com/Synth_Alice/status/777" || pin["text"] != "Pinned via timeline" {
		t.Errorf("pinned_post = %#v", data["pinned_post"])
	}
	if got := p.count("pinned"); got != 2 {
		t.Errorf("pinned text read %d times, want 2 (polled until the timeline rendered)", got)
	}
}

func TestHealthGetProfileHeaderOnlyParsesCounts(t *testing.T) {
	healthFast(t)
	p := healthProfilePage(t, "header_only")
	data, err := New().GetProfile(context.Background(), p, "https://twitter.com/synth_bob?s=20")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"username":          "synth_bob",
		"full_name":         "Bob Synth",
		"bio":               "Plain bio",
		"followers_count":   int64(1200),
		"following_count":   int64(3456),
		"content_count":     int64(12000000),
		"join_date":         "2011-03",
		"birth_date":        "June 5",
		"profile_category":  "Science & Technology",
		"verification_type": "business",
		"is_verified":       true,
		"is_protected":      true,
		"can_dm":            false,
		"banner_url":        "",
	}
	for k, v := range want {
		if !reflect.DeepEqual(data[k], v) {
			t.Errorf("%s = %#v, want %#v", k, data[k], v)
		}
	}
	if !reflect.DeepEqual(data["pinned_post"], map[string]interface{}{"url": "https://x.com/synth_bob/status/555", "text": "Pinned hello"}) {
		t.Errorf("pinned_post = %#v", data["pinned_post"])
	}
	if !reflect.DeepEqual(data["links"], []map[string]interface{}{{"url": "https://t.co/bob", "title": "bob.example"}}) {
		t.Errorf("links = %#v", data["links"])
	}
	if _, ok := data["platform_id"]; ok {
		t.Error("platform_id set without a user object")
	}
	if p.count("pinned") != 0 {
		t.Error("pinned text polled although the header had it")
	}
}

func TestHealthGetProfileIgnoresOtherUsersObject(t *testing.T) {
	healthFast(t)
	p := healthProfilePage(t, "other_user")
	data, err := New().GetProfile(context.Background(), p, "synth_carol")
	if err != nil {
		t.Fatal(err)
	}
	if data["full_name"] != "Carol Header" || data["bio"] != "header bio" {
		t.Errorf("another account's user object was applied: %v", data)
	}
	if _, ok := data["followers_count"]; ok {
		t.Errorf("followers_count = %v from a non-count text", data["followers_count"])
	}
	if _, ok := data["verification_type"]; ok {
		t.Error("verification_type set for an unverified profile")
	}
}

func TestHealthGetProfileDataUsesPageURLWhenHeaderHasNoHandle(t *testing.T) {
	healthFast(t)
	p := healthProfilePage(t, "no_handle")
	p.url = "https://x.com/synth_dana"
	data, err := New().GetProfileData(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if data["username"] != "synth_dana" || data["profile_url"] != "https://x.com/synth_dana" || data["followers_count"] != int64(60700000) {
		t.Errorf("data = %v", data)
	}
	if len(p.navs) != 0 {
		t.Errorf("GetProfileData navigated: %v", p.navs)
	}
}

func TestHealthGetProfileErrors(t *testing.T) {
	healthFast(t)
	ctx := context.Background()

	t.Run("unavailable account", func(t *testing.T) {
		p := healthProfilePage(t, "empty_state")
		_, err := New().GetProfile(ctx, p, "synth_gone")
		healthWantErr(t, err, "profile https://x.com/synth_gone is unavailable: This account doesn’t exist")
	})

	t.Run("no name and no username", func(t *testing.T) {
		p := healthProfilePage(t, "nameless")
		p.url = "https://x.com/home"
		_, err := New().GetProfileData(ctx, p)
		healthWantErr(t, err, "no name or username")
	})

	t.Run("redirected to login", func(t *testing.T) {
		p := healthProfilePage(t, "with_user")
		p.redirects["https://x.com/synth_alice"] = "https://x.com/i/flow/login?redirect_after_login=%2Fsynth_alice"
		_, err := New().GetProfile(ctx, p, "synth_alice")
		if !errors.Is(err, errNotLoggedIn) {
			t.Fatalf("err = %v, want errNotLoggedIn", err)
		}
		if p.count("scrape") != 0 {
			t.Error("scraped the login page")
		}
	})

	t.Run("header never renders", func(t *testing.T) {
		p := newHealthPage("about:blank")
		p.on("ready", profileReadyJS, healthConst(""))
		_, err := New().GetProfile(ctx, p, "synth_slow")
		healthWantErr(t, err, "profile https://x.com/synth_slow did not render")
		if !errors.Is(err, errTimeout) {
			t.Errorf("err = %v, want a timeout", err)
		}
	})

	t.Run("bounced to onboarding while waiting", func(t *testing.T) {
		p := newHealthPage("https://x.com/i/jf/onboarding/web")
		p.on("ready", profileReadyJS, healthConst(""))
		if _, err := New().GetProfileData(ctx, p); !errors.Is(err, errNotLoggedIn) {
			t.Fatalf("err = %v, want errNotLoggedIn", err)
		}
	})

	t.Run("scrape script fails", func(t *testing.T) {
		p := newHealthPage("about:blank")
		p.on("ready", profileReadyJS, healthConst("profile"))
		p.on("scrape", scrapeProfileJS, healthConst(nil))
		_, err := New().GetProfile(ctx, p, "synth_alice")
		healthWantErr(t, err, "read profile https://x.com/synth_alice")
	})

	t.Run("bad target", func(t *testing.T) {
		p := newHealthPage("about:blank")
		_, err := New().GetProfile(ctx, p, "https://example.com/jack")
		healthWantErr(t, err, "not an x.com profile URL")
		if len(p.navs) != 0 {
			t.Errorf("navigated for a bad target: %v", p.navs)
		}
	})

	t.Run("page URL unreadable", func(t *testing.T) {
		p := newHealthPage("")
		p.urlErr = errors.New("tab gone")
		_, err := New().GetProfileData(ctx, p)
		healthWantErr(t, err, "read page URL: tab gone")
	})
}

func TestHealthUserApplyVerificationAndDates(t *testing.T) {
	i64 := func(n int64) *int64 { return &n }
	cases := []struct {
		name string
		u    xUser
		want map[string]interface{}
	}{
		{"business badge, RFC3339 date", xUser{VerifiedType: "Business", CreatedAt: "2012-04-05T06:07:08Z"},
			map[string]interface{}{"verification_type": "business", "is_verified": true, "join_date": "2012-04-05"}},
		{"legacy verified", xUser{Verified: true},
			map[string]interface{}{"verification_type": "verified", "is_verified": true}},
		{"protected, unverified", xUser{Protected: true, FollowersCount: i64(0)},
			map[string]interface{}{"is_protected": true, "followers_count": int64(0)}},
	}
	for _, c := range cases {
		data := map[string]interface{}{"bio": "keep"}
		c.u.apply(data)
		for k, v := range c.want {
			if !reflect.DeepEqual(data[k], v) {
				t.Errorf("%s: %s = %#v, want %#v", c.name, k, data[k], v)
			}
		}
		if data["bio"] != "keep" {
			t.Errorf("%s: header bio overwritten: %v", c.name, data["bio"])
		}
	}

	// Unparseable dates and impossible birthdays are ignored.
	u := xUser{CreatedAt: "sometime", Birthdate: &struct {
		Day   int `json:"day"`
		Month int `json:"month"`
		Year  int `json:"year"`
	}{Day: 3, Month: 13}}
	data := map[string]interface{}{"join_date": "2011-03"}
	u.apply(data)
	if data["join_date"] != "2011-03" {
		t.Errorf("join_date = %v", data["join_date"])
	}
	if _, ok := data["birth_date"]; ok {
		t.Errorf("birth_date = %v from month 13", data["birth_date"])
	}
	if data["is_protected"] != false {
		t.Errorf("is_protected = %v", data["is_protected"])
	}
	if _, ok := data["is_verified"]; ok {
		t.Error("is_verified set without a badge")
	}
}

func TestHealthLargeAvatarAndJoinDate(t *testing.T) {
	for in, want := range map[string]string{
		"https://pbs.twimg.com/p/a_normal.jpg":   "https://pbs.twimg.com/p/a_400x400.jpg",
		"https://pbs.twimg.com/p/a_bigger.png":   "https://pbs.twimg.com/p/a_400x400.png",
		"https://pbs.twimg.com/p/a_mini.jpeg":    "https://pbs.twimg.com/p/a_400x400.jpeg",
		"https://pbs.twimg.com/p/a_200x200.jpg":  "https://pbs.twimg.com/p/a_400x400.jpg",
		"https://pbs.twimg.com/p/a.jpg":          "https://pbs.twimg.com/p/a.jpg",
		"https://pbs.twimg.com/p/normal_pic.jpg": "https://pbs.twimg.com/p/normal_pic.jpg",
	} {
		if got := largeAvatar(in); got != want {
			t.Errorf("largeAvatar(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"Joined March 2011":     "2011-03",
		" joined december 1999": "1999-12",
		"Joined Foo 2011":       "Joined Foo 2011",
		"Joined recently":       "Joined recently",
		"":                      "",
	} {
		if got := joinDate(in); got != want {
			t.Errorf("joinDate(%q) = %q, want %q", in, got, want)
		}
	}
	if firstNonEmptyStr("", "  ", "a", "b") != "a" || firstNonEmptyStr() != "" {
		t.Error("firstNonEmptyStr")
	}
}

// ---------------------------------------------------------------------------
// search_posts
// ---------------------------------------------------------------------------

func healthSearchPage(t *testing.T, pages ...string) *healthPage {
	p := newHealthPage("about:blank")
	var vs []interface{}
	for _, name := range pages {
		vs = append(vs, healthFixture(t, "health_search.json", name))
	}
	p.on("tweets", tweetsJS, healthSeq(vs...))
	p.on("scroll", scrollJS, healthConst(true))
	return p
}

func TestHealthSearchPostsSkipsAdsDedupesAndParsesCounts(t *testing.T) {
	healthFast(t)
	p := healthSearchPage(t, "page1", "page2")
	rows, err := New().SearchPosts(context.Background(), p, "  synthkeyword ", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := healthIDs(rows, "id"); !reflect.DeepEqual(got, []string{"2001", "2002", "2003"}) {
		t.Fatalf("ids = %v", got)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://x.com/search?q=synthkeyword"}) {
		t.Errorf("navs = %v", p.navs)
	}
	if p.count("scroll") != 1 {
		t.Errorf("scrolled %d times, want 1", p.count("scroll"))
	}
	r0 := rows[0]
	want0 := map[string]interface{}{
		"url": "https://x.com/synth_erin/status/2001", "id": "2001", "author_username": "synth_erin", "author_name": "Synth Erin",
		"author_url": "https://x.com/synth_erin", "post_text": "Quiet morning", "created_at": "2026-09-20T10:00:00.000Z",
		"liked": false, "keyword": "synthkeyword", "reply_count": int64(5), "repost_count": int64(2), "like_count": int64(1200),
	}
	if !reflect.DeepEqual(r0, want0) {
		t.Errorf("row 0 = %#v", r0)
	}
	r1 := rows[1]
	if r1["like_count"] != int64(12000000) || r1["repost_count"] != int64(3456) || r1["liked"] != true {
		t.Errorf("row 1 = %v", r1)
	}
	if _, ok := r1["reply_count"]; ok {
		t.Errorf("reply_count set from an empty counter: %v", r1["reply_count"])
	}
	r2 := rows[2]
	if _, ok := r2["author_url"]; ok {
		t.Errorf("author_url without an author: %v", r2)
	}
	if _, ok := r2["like_count"]; ok {
		t.Errorf("like_count from %q", "Like")
	}
}

func TestHealthSearchPostsStopsWhenTimelineStopsGrowing(t *testing.T) {
	healthFast(t)
	p := healthSearchPage(t, "page1")
	rows, err := New().SearchPosts(context.Background(), p, "synthkeyword", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := healthIDs(rows, "id"); !reflect.DeepEqual(got, []string{"2001", "2002"}) {
		t.Fatalf("ids = %v", got)
	}
	// One scroll after the first page, then two more that add nothing; the
	// third empty read ends the search.
	if got := p.count("scroll"); got != 3 {
		t.Errorf("scrolled %d times, want 3", got)
	}
}

func TestHealthSearchPostsSeveralQueries(t *testing.T) {
	healthFast(t)
	p := healthSearchPage(t, "page1", "page2")
	rows, err := New().SearchPosts(context.Background(), p, "alpha\n\n  beta two \n", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://x.com/search?q=alpha", "https://x.com/search?q=beta+two"}) {
		t.Errorf("navs = %v", p.navs)
	}
	// max applies per query; a post already returned for "alpha" is not
	// repeated for "beta two".
	if got := healthIDs(rows, "id"); !reflect.DeepEqual(got, []string{"2001", "2002", "2003", "2004"}) {
		t.Fatalf("ids = %v", got)
	}
	if got := healthIDs(rows, "keyword"); !reflect.DeepEqual(got, []string{"alpha", "alpha", "beta two", "beta two"}) {
		t.Errorf("keywords = %v", got)
	}
}

func TestHealthSearchPostsEmptyAndErrors(t *testing.T) {
	healthFast(t)
	ctx := context.Background()

	t.Run("no results is an empty list", func(t *testing.T) {
		p := healthSearchPage(t, "empty")
		rows, err := New().SearchPosts(ctx, p, "zzz", 0)
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows = %v, err = %v", rows, err)
		}
		if p.count("scroll") != 0 {
			t.Error("scrolled an empty result page")
		}
	})

	t.Run("query required", func(t *testing.T) {
		for _, q := range []string{"", " \n \t\n"} {
			_, err := New().SearchPosts(ctx, newHealthPage("about:blank"), q, 5)
			healthWantErr(t, err, "search query is required")
		}
	})

	t.Run("nothing rendered", func(t *testing.T) {
		p := healthSearchPage(t, "blank")
		_, err := New().SearchPosts(ctx, p, "synthkeyword", 5)
		healthWantErr(t, err, `search "synthkeyword": no results rendered`)
	})

	t.Run("login wall", func(t *testing.T) {
		p := healthSearchPage(t, "blank")
		p.redirects["https://x.com/search?q=synthkeyword"] = "https://x.com/login"
		if _, err := New().SearchPosts(ctx, p, "synthkeyword", 5); !errors.Is(err, errNotLoggedIn) {
			t.Fatalf("err = %v, want errNotLoggedIn", err)
		}
	})

	t.Run("scroll fails keeps the rows read so far", func(t *testing.T) {
		p := newHealthPage("about:blank")
		p.on("tweets", tweetsJS, healthConst(healthFixture(t, "health_search.json", "page1")))
		rows, err := New().SearchPosts(ctx, p, "synthkeyword", 10)
		healthWantErr(t, err, "scroll search results")
		if got := healthIDs(rows, "id"); !reflect.DeepEqual(got, []string{"2001", "2002"}) {
			t.Errorf("partial ids = %v", got)
		}
	})

	t.Run("re-read fails", func(t *testing.T) {
		p := healthSearchPage(t, "page1")
		p.rules[0].reply = func(n int, _ []interface{}) (interface{}, error) {
			if n == 1 {
				return healthFixture(t, "health_search.json", "page1"), nil
			}
			return nil, nil
		}
		rows, err := New().SearchPosts(ctx, p, "synthkeyword", 10)
		healthWantErr(t, err, "read search results")
		if len(rows) != 2 {
			t.Errorf("rows = %d", len(rows))
		}
	})
}

func TestHealthSearchPostsViaMethod(t *testing.T) {
	healthFast(t)
	fn, _ := New().GetMethodByName("search_posts")
	res, err := fn(context.Background(), healthSearchPage(t, "page1", "page2"), "synthkeyword", "2")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := res.([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("result = %#v", res)
	}
	if _, ok := list[0].(map[string]interface{}); !ok {
		t.Errorf("row type %T", list[0])
	}
	// An invalid max falls back to the default (20): all 4 posts.
	res, err = fn(context.Background(), healthSearchPage(t, "page1", "page2"), "synthkeyword", "lots")
	if err != nil || len(res.([]interface{})) != 4 {
		t.Fatalf("default max: %v, %v", res, err)
	}
	// Errors pass through listResult as a nil result.
	res, err = fn(context.Background(), healthSearchPage(t, "blank"), "synthkeyword")
	if err == nil || res != nil {
		t.Fatalf("res = %v, err = %v", res, err)
	}
}

// ---------------------------------------------------------------------------
// list_followers
// ---------------------------------------------------------------------------

func healthFollowersPage(t *testing.T, pages ...string) *healthPage {
	p := newHealthPage("about:blank")
	var vs []interface{}
	for _, name := range pages {
		vs = append(vs, healthFixture(t, "health_followers.json", name))
	}
	p.on("cells", userCellsJS, healthSeq(vs...))
	p.on("scroll", scrollJS, healthConst(true))
	return p
}

func TestHealthListFollowersMapsAndDedupes(t *testing.T) {
	healthFast(t)
	p := healthFollowersPage(t, "page1", "page2")
	rows, err := New().ListFollowers(context.Background(), p, "https://x.com/synth_alice/status/1", "VERIFIED_FOLLOWERS_FETCH", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://x.com/synth_alice/verified_followers"}) {
		t.Errorf("navs = %v", p.navs)
	}
	if got := healthIDs(rows, "username"); !reflect.DeepEqual(got, []string{"synth_ivy", "synth_jon", "synth_kim"}) {
		t.Fatalf("usernames = %v (case-insensitive dedupe, max 3)", got)
	}
	want := map[string]interface{}{
		"username": "synth_ivy", "profile_url": "https://x.com/synth_ivy", "full_name": "Ivy", "bio": "Gardener",
		"is_following": true, "follows_you": true, "is_verified": true, "profile_picture_url": "https://pbs.twimg.com/i.jpg",
		"source": "verified_followers", "source_username": "synth_alice",
	}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("row 0 = %#v", rows[0])
	}
}

func TestHealthListFollowersEndsAndErrors(t *testing.T) {
	healthFast(t)
	ctx := context.Background()

	t.Run("following list ends", func(t *testing.T) {
		p := healthFollowersPage(t, "page1")
		rows, err := New().ListFollowers(ctx, p, "synth_alice", "following", 0)
		if err != nil || len(rows) != 2 || rows[0]["source"] != "following" {
			t.Fatalf("rows = %v, err = %v", rows, err)
		}
		if p.count("scroll") != 3 {
			t.Errorf("scrolled %d times, want 3", p.count("scroll"))
		}
	})

	t.Run("empty list", func(t *testing.T) {
		p := healthFollowersPage(t, "empty")
		rows, err := New().ListFollowers(ctx, p, "synth_alice", "", 10)
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows = %v, err = %v", rows, err)
		}
		if p.navs[0] != "https://x.com/synth_alice/followers" {
			t.Errorf("navs = %v", p.navs)
		}
	})

	t.Run("unknown source type", func(t *testing.T) {
		p := healthFollowersPage(t, "page1")
		_, err := New().ListFollowers(ctx, p, "synth_alice", "LIKERS", 10)
		healthWantErr(t, err, `unknown sourceType "LIKERS"`)
		if len(p.navs) != 0 {
			t.Errorf("navigated: %v", p.navs)
		}
	})

	t.Run("bad profile", func(t *testing.T) {
		_, err := New().ListFollowers(ctx, newHealthPage(""), "", "", 10)
		healthWantErr(t, err, "profile URL or username is required")
	})

	t.Run("never renders", func(t *testing.T) {
		p := healthFollowersPage(t, "not_ready")
		_, err := New().ListFollowers(ctx, p, "synth_alice", "", 10)
		healthWantErr(t, err, "followers list of @synth_alice did not render")
	})

	t.Run("login wall", func(t *testing.T) {
		p := healthFollowersPage(t, "not_ready")
		p.redirects["https://x.com/synth_alice/followers"] = "https://x.com/i/flow/login"
		if _, err := New().ListFollowers(ctx, p, "synth_alice", "", 10); !errors.Is(err, errNotLoggedIn) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("scroll fails", func(t *testing.T) {
		p := newHealthPage("about:blank")
		p.on("cells", userCellsJS, healthConst(healthFixture(t, "health_followers.json", "page1")))
		rows, err := New().ListFollowers(ctx, p, "synth_alice", "", 10)
		healthWantErr(t, err, "scroll followers list")
		if len(rows) != 2 {
			t.Errorf("rows = %d", len(rows))
		}
	})
}

// ---------------------------------------------------------------------------
// list_conversations
// ---------------------------------------------------------------------------

func healthInboxPage(t *testing.T, snaps ...string) *healthPage {
	p := newHealthPage("about:blank")
	var vs []interface{}
	for _, name := range snaps {
		vs = append(vs, healthFixture(t, "health_conversations.json", name))
	}
	p.on("conversations", conversationsJS, healthSeq(vs...))
	return p
}

func TestHealthListConversationsFiltersAndRereads(t *testing.T) {
	healthFast(t)
	ctx := context.Background()

	p := healthInboxPage(t, "first", "full")
	rows, err := New().ListConversations(ctx, p, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://x.com/messages"}) {
		t.Errorf("navs = %v", p.navs)
	}
	// The second, fuller read wins; the conversation whose last message is
	// the viewer's own is skipped.
	if got := healthIDs(rows, "name"); !reflect.DeepEqual(got, []string{"Ivy", "Kim", "Group"}) {
		t.Fatalf("names = %v", got)
	}
	if !reflect.DeepEqual(rows[0], map[string]interface{}{"url": "https://x.com/messages/1-2", "name": "Ivy", "preview": "hey there", "unread": true}) {
		t.Errorf("row 0 = %#v", rows[0])
	}

	rows, err = New().ListConversations(ctx, healthInboxPage(t, "full"), 20, true)
	if err != nil || !reflect.DeepEqual(healthIDs(rows, "name"), []string{"Ivy", "Group"}) {
		t.Fatalf("unread only: %v, %v", healthIDs(rows, "name"), err)
	}
	rows, err = New().ListConversations(ctx, healthInboxPage(t, "full"), 1, false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("max 1: %v, %v", rows, err)
	}
	// A shorter second read (rows still rendering) does not replace the first.
	rows, err = New().ListConversations(ctx, healthInboxPage(t, "full", "first"), 20, false)
	if err != nil || len(rows) != 3 {
		t.Fatalf("shorter re-read: %v, %v", healthIDs(rows, "name"), err)
	}
}

func TestHealthListConversationsErrors(t *testing.T) {
	healthFast(t)
	ctx := context.Background()

	if _, err := New().ListConversations(ctx, healthInboxPage(t, "locked"), 5, true); !errors.Is(err, errChatLocked) {
		t.Errorf("locked: err = %v", err)
	}
	_, err := New().ListConversations(ctx, healthInboxPage(t, "cells_only"), 5, true)
	healthWantErr(t, err, "inbox shows 3 conversations but no conversation links")

	p := newHealthPage("")
	p.on("conversations", conversationsJS, healthConst(nil))
	_, err = New().ListConversations(ctx, p, 5, true)
	healthWantErr(t, err, "inbox did not render")

	p = healthInboxPage(t, "cells_only")
	p.redirects["https://x.com/messages"] = "https://x.com/i/flow/login"
	if _, err := New().ListConversations(ctx, p, 5, true); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("login: err = %v", err)
	}
}

func TestHealthListConversationsViaMethod(t *testing.T) {
	healthFast(t)
	fn, _ := New().GetMethodByName("list_conversations")
	res, err := fn(context.Background(), healthInboxPage(t, "full"), "", "no")
	if err != nil {
		t.Fatal(err)
	}
	if list := res.([]interface{}); len(list) != 3 {
		t.Errorf("unreadOnly=no returned %d rows, want 3", len(list))
	}
	res, err = fn(context.Background(), healthInboxPage(t, "full"))
	if err != nil {
		t.Fatal(err)
	}
	if list := res.([]interface{}); len(list) != 2 {
		t.Errorf("default unreadOnly returned %d rows, want 2", len(list))
	}
}

// ---------------------------------------------------------------------------
// IsLoggedIn
// ---------------------------------------------------------------------------

func TestHealthIsLoggedIn(t *testing.T) {
	b := New()
	cases := []struct {
		name string
		url  string
		has  []string
		want bool
	}{
		{"signed-in nav", "https://x.com/home", []string{"[data-testid='SideNav_NewTweet_Button']"}, true},
		{"mobile tab bar", "https://x.com/home", []string{"[data-testid='AppTabBar_Profile_Link']"}, true},
		{"login form wins", "https://x.com/home", []string{"input[autocomplete='username']", "[data-testid='SideNav_AccountSwitcher_Button']"}, false},
		{"login flow URL", "https://x.com/i/flow/login", []string{"[data-testid='SideNav_AccountSwitcher_Button']"}, false},
		{"nothing signed-in", "https://x.com/explore", nil, false},
	}
	for _, c := range cases {
		p := newHealthPage(c.url)
		for _, s := range c.has {
			p.has[s] = true
		}
		got, err := b.IsLoggedIn(p)
		if err != nil || got != c.want {
			t.Errorf("%s: IsLoggedIn = %v, %v; want %v", c.name, got, err, c.want)
		}
	}
	p := newHealthPage("")
	p.urlErr = errors.New("detached")
	if _, err := b.IsLoggedIn(p); err == nil {
		t.Error("URL error swallowed")
	}
	if b.LoginURL() != "https://x.com/i/flow/login" {
		t.Errorf("LoginURL = %q", b.LoginURL())
	}
}

func TestHealthHelpers(t *testing.T) {
	b := New()
	if got := b.SearchURL("  a b&c "); got != "https://x.com/search?q=a+b%26c" {
		t.Errorf("SearchURL = %q", got)
	}
	for in, want := range map[string]int{"5": 5, " 7 ": 7, "": 20, "0": 20, "-3": 20, "x": 20} {
		if got := intArg(in, 20); got != want {
			t.Errorf("intArg(%q) = %d", in, got)
		}
	}
	for in, want := range map[string]bool{"TRUE": true, "on": true, "y": true, "0": false, "Off": false, "n": false, "maybe": true, "": true} {
		if got := boolArg(in, true); got != want {
			t.Errorf("boolArg(%q) = %v", in, got)
		}
	}
	if boolArg("maybe", false) {
		t.Error("boolArg default")
	}
	if got := mediaPaths([]string{" a ", "", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("mediaPaths([]string) = %q", got)
	}
	if got := mediaPaths(42); got != nil {
		t.Errorf("mediaPaths(int) = %q", got)
	}
	if got := truncateForError("héllo wörld", 5); got != "héllo…" {
		t.Errorf("truncateForError runes = %q", got)
	}
	if got := verifySnippet("  a\n\n b  " + string(make([]rune, 0))); got != "a b" {
		t.Errorf("verifySnippet = %q", got)
	}
	long := ""
	for i := 0; i < 50; i++ {
		long += "é"
	}
	if got := []rune(verifySnippet(long)); len(got) != 40 {
		t.Errorf("verifySnippet keeps %d runes, want 40", len(got))
	}
	if got := messageSnippet("  hi  "); got != "hi" {
		t.Errorf("messageSnippet = %q", got)
	}
	if markedSelector("tok") != "[data-monoagent-x='tok']" {
		t.Error("markedSelector")
	}
	if a, b2 := token(), token(); a == b2 || len(a) != 16 {
		t.Errorf("token() = %q, %q", a, b2)
	}
	for _, info := range []pickInfo{
		{Tag: "BUTTON", TestID: "sendDMFromProfile", Label: "Message"},
		{Tag: "DIV", Role: "Button", Label: "Message"},
	} {
		if !isMessageButton(info) {
			t.Errorf("isMessageButton(%+v) = false", info)
		}
	}
	for _, info := range []pickInfo{
		{Tag: "A", Label: "Message"},
		{Tag: "BUTTON", TestID: "1234-follow", Label: "Follow"},
		{Tag: "BUTTON", Label: "Subscribe to @x"},
		{Tag: "BUTTON", TestID: "userActions", Label: "More"},
		{Tag: "BUTTON", Label: "Turn on notifications"},
		{Tag: "BUTTON", Label: "Mute @x"},
	} {
		if isMessageButton(info) {
			t.Errorf("isMessageButton(%+v) = true", info)
		}
	}
}
