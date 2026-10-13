//go:build !nosocial

package linkedin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHealthExtractUsername(t *testing.T) {
	b := &LinkedInBot{}
	for in, want := range map[string]string{
		"https://www.linkedin.com/in/jane-doe-123/":                     "jane-doe-123",
		"https://www.linkedin.com/in/jane-doe-123":                      "jane-doe-123",
		"https://www.linkedin.com/in/jane-doe-123/recent-activity/all/": "jane-doe-123",
		"https://www.linkedin.com/in/jane-doe-123/?trk=x#about":         "jane-doe-123",
		"/in/relative-slug/":                    "relative-slug",
		"https://www.linkedin.com/in/":          "",
		"https://www.linkedin.com/company/acme": "",
		"https://www.linkedin.com/":             "",
		"":                                      "",
		"://bad url":                            "",
	} {
		if got := b.ExtractUsername(in); got != want {
			t.Errorf("ExtractUsername(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthResolveAndSearchURL(t *testing.T) {
	b := &LinkedInBot{}
	if got := b.ResolveURL("/in/x/"); got != "https://www.linkedin.com/in/x/" {
		t.Errorf("ResolveURL relative = %q", got)
	}
	for _, abs := range []string{"https://www.linkedin.com/in/x/", "in/x", ""} {
		if got := b.ResolveURL(abs); got != abs {
			t.Errorf("ResolveURL(%q) = %q, want unchanged", abs, got)
		}
	}
	if got := b.SearchURL("  go & rust devs "); got != "https://www.linkedin.com/search/results/people/?keywords=go+%26+rust+devs" {
		t.Errorf("SearchURL = %q", got)
	}
	if b.Platform() != "LINKEDIN" || b.LoginURL() != "https://www.linkedin.com/login" {
		t.Errorf("Platform/LoginURL = %q %q", b.Platform(), b.LoginURL())
	}
}

func TestHealthPostURLAndID(t *testing.T) {
	if got := postURL("urn:li:activity:7123456789"); got != "https://www.linkedin.com/feed/update/urn:li:activity:7123456789/" {
		t.Errorf("postURL = %q", got)
	}
	for in, want := range map[string]string{
		"https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/":      "7123456789012345678",
		"https://www.linkedin.com/feed/update/urn:li:ugcPost:7123456789012345679/":       "7123456789012345679",
		"urn:li:share:7123456789012345680":                                               "7123456789012345680",
		"https://www.linkedin.com/posts/ann-lee_title-activity-7123456789012345681-AbCd": "7123456789012345681",
		"https://www.linkedin.com/feed/update/urn:li:activity:12345/":                    "", // under 6 digits
		"https://www.linkedin.com/in/ann-lee-test/":                                      "",
		"": "",
	} {
		if got := postIDFromURL(in); got != want {
			t.Errorf("postIDFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthMessagingHelpers(t *testing.T) {
	if got := orUnknown("  "); got != "the intended recipient" {
		t.Errorf("orUnknown(blank) = %q", got)
	}
	if got := orUnknown("Ann Lee"); got != "Ann Lee" {
		t.Errorf("orUnknown(name) = %q", got)
	}
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello…"},
		{"héllo wörld", 4, "héll…"}, // runes, not bytes
		{"日本語のテキスト", 3, "日本語…"},
		{"", 3, ""},
		{"abc", 0, "…"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	long := strings.Repeat("word ", 30)
	if got := messageSnippet(long); got != strings.Repeat("word ", 12) {
		t.Errorf("messageSnippet(long) = %q (%d runes), want the first 60 runes", got, len([]rune(got)))
	}
	if got := snippet("  a  b\n\tc  "); got != "a b c" {
		t.Errorf("snippet normalises whitespace and nbsp: %q", got)
	}
	if got := normSpace("x  y  z"); got != "x y z" {
		t.Errorf("normSpace = %q", got)
	}
}

func TestHealthListKindAndArgs(t *testing.T) {
	if listKind("https://www.linkedin.com/mynetwork/network-manager/people-follow/following/") != "following" {
		t.Error("following URL not recognised")
	}
	if listKind("https://www.linkedin.com/mynetwork/network-manager/people-follow/followers/") != "followers" {
		t.Error("followers URL not recognised")
	}
	if n, err := intArg(" 7.9 ", 1); err != nil || n != 7 {
		t.Errorf("intArg(7.9) = %d, %v; want 7 (truncated)", n, err)
	}
	if _, err := intArg("ten", 1); err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("intArg(ten) err = %v", err)
	}
	for in, want := range map[string]bool{"YES": true, " on ": true, "y": true, "off": false, "N": false} {
		if got, err := boolArg(in, !want); err != nil || got != want {
			t.Errorf("boolArg(%q) = %v, %v", in, got, err)
		}
	}
}

func TestHealthJSONHelpers(t *testing.T) {
	b, err := jsonMarshal(map[string]int{"a": 1})
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("jsonMarshal = %s, %v", b, err)
	}
	var v struct{ A int }
	if err := jsonUnmarshal(`{"A":2}`, &v); err != nil || v.A != 2 {
		t.Fatalf("jsonUnmarshal = %+v, %v", v, err)
	}
	if err := jsonUnmarshal(`{`, &v); err == nil {
		t.Fatal("jsonUnmarshal accepted broken JSON")
	}
}

func TestHealthAsFromJSONListRoundTrip(t *testing.T) {
	in := []Position{
		{Title: "Staff Engineer", Company: "Acme", DateRange: "Jan 2022 - Present", Start: "Jan 2022", End: "Present"},
		{Title: "Engineer", Company: "Beta"},
	}
	list := asJSONList(in)
	if len(list) != 2 {
		t.Fatalf("asJSONList len = %d", len(list))
	}
	m, ok := list[1].(map[string]interface{})
	if !ok || m["title"] != "Engineer" || m["company"] != "Beta" {
		t.Fatalf("asJSONList[1] = %#v", list[1])
	}
	if _, has := m["date_range"]; has {
		t.Error("empty date_range should be omitted (omitempty)")
	}
	if back := fromJSONList[Position](list); !reflect.DeepEqual(back, in) {
		t.Errorf("fromJSONList round trip = %+v", back)
	}
	// nil (JSON null) and unmarshalable input give an empty list.
	if got := asJSONList(nil); len(got) != 0 {
		t.Errorf("asJSONList(nil) = %#v", got)
	}
	if got := asJSONList(func() {}); len(got) != 0 {
		t.Errorf("asJSONList(func) = %#v", got)
	}
	if got := fromJSONList[School](nil); len(got) != 0 {
		t.Errorf("fromJSONList(nil) = %#v", got)
	}
}

func TestHealthSetDetails(t *testing.T) {
	m := map[string]interface{}{"headline": "Builder"}
	setDetails(m, []Position{{Title: "Founder", Company: "Old Co", DateRange: "2010 - 2012", End: "2012"}}, nil)
	if m["job_title"] != "Builder" || m["current_company"] != "" {
		t.Errorf("no current position: job_title=%v current_company=%v", m["job_title"], m["current_company"])
	}
	if exp := m["experience"].([]interface{}); len(exp) != 1 {
		t.Errorf("experience = %#v", exp)
	}
	if edu := m["education"].([]interface{}); len(edu) != 0 {
		t.Errorf("education should be empty, got %#v", edu)
	}

	setDetails(m, []Position{
		{Title: "Advisor", Company: "Past", DateRange: "2015 - 2016", End: "2016"},
		{Title: "CTO", Company: "Now Co", DateRange: "2020 - Present", End: "Present"},
		{Title: "Board", Company: "Also Now", DateRange: "2021 - Present", End: "Present"},
	}, []School{{School: "Uni"}})
	if m["job_title"] != "CTO" || m["current_company"] != "Now Co" {
		t.Errorf("current position: job_title=%v current_company=%v", m["job_title"], m["current_company"])
	}
}

func TestHealthCurrentPositionEdges(t *testing.T) {
	for _, p := range []Position{
		{Title: "single year", DateRange: "2019", Start: "2019"},
		{Title: "no dates", End: "Present"},
		{Title: "ended", DateRange: "Jan 2020 - Mar 2021", End: "Mar 2021"},
	} {
		if _, ok := currentPosition([]Position{p}); ok {
			t.Errorf("%q read as current", p.Title)
		}
	}
	if p, ok := currentPosition([]Position{{Title: "now", DateRange: "2024 - heute", End: "heute"}}); !ok || p.Title != "now" {
		t.Error("a localised open end should count as current")
	}
}

func TestHealthItemMappers(t *testing.T) {
	r := rawPost{URN: "urn:li:activity:7123456789", ID: "7123456789", Author: "Acme",
		AuthorURL: "https://www.linkedin.com/company/acme-test/posts", Text: "hi", Timestamp: "2d", Likes: 3, Comments: 2, Reposts: 1}
	want := map[string]interface{}{
		"url": "https://www.linkedin.com/feed/update/urn:li:activity:7123456789/", "urn": "urn:li:activity:7123456789",
		"activity_id": "7123456789", "shortcode": "7123456789", "text_preview": "hi", "author": "Acme",
		"author_url": "https://www.linkedin.com/company/acme-test", "timestamp": "2d",
		"likes_count": 3, "comments_count": 2, "reposts_count": 1,
	}
	if got := r.item(); !reflect.DeepEqual(got, want) {
		t.Errorf("rawPost.item = %#v", got)
	}
	p := (&LinkedInBot{}).personItem(rawPerson{URL: "https://www.linkedin.com/in/bo-kim-test", Name: "Bo", Headline: "Designer", Location: "Seoul", Degree: "3rd+"})
	if p["username"] != "bo-kim-test" || p["job_title"] != "Designer" || p["profile_url"] != p["url"] || p["platform"] != "LINKEDIN" || p["connection_degree"] != "3rd+" {
		t.Errorf("personItem = %#v", p)
	}
}

func TestHealthSleepAndPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx(cancelled, 0) = %v", err)
	}
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx(cancelled, 1h) = %v", err)
	}
	if err := sleepCtx(context.Background(), -time.Second); err != nil {
		t.Errorf("sleepCtx(negative) = %v", err)
	}

	healthTimings(t)
	n := 0
	if err := poll(context.Background(), time.Second, func() (bool, error) { n++; return n == 3, nil }); err != nil || n != 3 {
		t.Errorf("poll until true: n=%d err=%v", n, err)
	}
	boom := errors.New("boom")
	if err := poll(context.Background(), time.Second, func() (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Errorf("poll cond error = %v", err)
	}
	if err := poll(context.Background(), 5*time.Millisecond, func() (bool, error) { return false, nil }); !errors.Is(err, errTimeout) {
		t.Errorf("poll timeout = %v", err)
	}
	if err := poll(ctx, time.Second, func() (bool, error) { return false, nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("poll cancelled = %v", err)
	}
}

func TestHealthIsLoggedIn(t *testing.T) {
	b := &LinkedInBot{}
	p := newHealthPage(t)
	if ok, _ := b.IsLoggedIn(p); ok {
		t.Error("blank page read as logged in")
	}
	p.has["#global-nav"] = true
	if ok, _ := b.IsLoggedIn(p); !ok {
		t.Error("global nav not read as logged in")
	}
	p.has["input#username"] = true
	if ok, _ := b.IsLoggedIn(p); ok {
		t.Error("login form must win over the nav")
	}
}

func TestHealthMethodArgErrors(t *testing.T) {
	b := &LinkedInBot{}
	if _, ok := b.GetMethodByName("no_such_method"); ok {
		t.Error("unknown method found")
	}
	p := newHealthPage(t)
	cases := []struct {
		method string
		args   []interface{}
		want   string
	}{
		{"list_user_posts", []interface{}{p, "https://www.linkedin.com/in/x/", "many"}, "maxCount"},
		{"list_post_comments", []interface{}{p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/", "x"}, "maxCount"},
		{"list_post_comments", []interface{}{p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/", "5", "perhaps"}, "includeReplies"},
		{"list_post_comments", []interface{}{p}, "postURL"},
		{"like_post", []interface{}{p}, "postURL"},
		{"comment_on_post", []interface{}{p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/"}, "commentText"},
		{"like_comment", []interface{}{p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/"}, "commentID"},
		{"send_message", []interface{}{p, "ann"}, "message"},
		{"reply_to_conversation", []interface{}{p, "https://www.linkedin.com/in/ann/", "hi"}, "not a messaging thread URL"},
		{"list_conversations", []interface{}{p, "lots"}, "max"},
		{"list_conversations", []interface{}{p, "5", "sometimes"}, "onlyUnread"},
		{"get_profile_data", []interface{}{p, "ann", "kinda"}, "includeDetails"},
		{"get_profile_data", []interface{}{p, " "}, "profileURL"},
		{"search_people", []interface{}{p, "go", "x"}, "max"},
		{"search_posts", []interface{}{p, "go", "x"}, "max"},
		{"search_posts", []interface{}{p, ""}, "keyword"},
		{"list_followers", []interface{}{p, "followers", "x"}, "max"},
		{"list_followers", []interface{}{p, "fans", "5"}, "unknown sourceType"},
		{"publish_post", []interface{}{p}, "text"},
		{"list_user_posts", []interface{}{"not a page", "x"}, "browser page"},
	}
	for _, c := range cases {
		fn, ok := b.GetMethodByName(c.method)
		if !ok {
			t.Fatalf("method %s missing", c.method)
		}
		_, err := fn(context.Background(), c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s%v: err = %v, want it to mention %q", c.method, c.args[1:], err, c.want)
		}
	}
	if navs := p.navigations(); len(navs) != 0 {
		t.Errorf("argument errors navigated the page: %v", navs)
	}
}

func TestHealthSendMessageValidation(t *testing.T) {
	b := &LinkedInBot{}
	p := newHealthPage(t)
	ctx := context.Background()
	if _, err := b.SendMessageTo(ctx, p, " ", "hi"); err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Errorf("blank recipient: %v", err)
	}
	if err := b.SendMessage(ctx, p, "ann", "  "); err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Errorf("blank message: %v", err)
	}
	if err := b.ReplyToConversation(ctx, p, "https://www.linkedin.com/messaging/thread/2-A/", " "); err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Errorf("blank reply: %v", err)
	}
	if _, err := b.CommentOnPost(ctx, p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/", " ", ""); err == nil {
		t.Error("blank comment accepted")
	}
	if _, err := b.LikePost(ctx, p, "https://www.linkedin.com/feed/update/urn:li:activity:1234567/", "wow"); err == nil || !strings.Contains(err.Error(), "unknown reaction") {
		t.Errorf("unknown reaction: %v", err)
	}
	if navs := p.navigations(); len(navs) != 0 {
		t.Errorf("validation errors navigated the page: %v", navs)
	}
}
