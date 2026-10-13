//go:build !nosocial

package hackernews

// Offline tests of the Go layer: pure helpers, and the read/write flows
// driven by a fake page whose Eval returns canned results shaped exactly
// like what the page scripts return. No browser, no network.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// healthEvalFunc answers one page script. script is the exact source handed
// to EvalJSON (a package constant for HN), args its arguments.
type healthEvalFunc func(p *healthPage, script string, args []interface{}) (interface{}, error)

// healthPage is a fake browser.PageInterface. It deliberately has no EvalCDP,
// so botpkg.EvalJSON falls back to Eval.
type healthPage struct {
	browser.PageInterface
	url     string
	navs    []string
	eval    healthEvalFunc
	els     map[string]*healthEl // Race/Has by selector
	onClick func(p *healthPage, el *healthEl)
	scripts []string
}

func (p *healthPage) Navigate(u string) error                     { p.url = u; p.navs = append(p.navs, u); return nil }
func (p *healthPage) WaitLoad() error                             { return nil }
func (p *healthPage) GetURL() (string, error)                     { return p.url, nil }
func (p *healthPage) Has(sel string) (bool, error)                { _, ok := p.els[sel]; return ok, nil }
func (p *healthPage) Timeout(time.Duration) browser.PageInterface { return p }

func (p *healthPage) Race(sels []string, _ time.Duration) (int, browser.ElementHandle, error) {
	for i, s := range sels {
		if el, ok := p.els[s]; ok {
			return i, el, nil
		}
	}
	return -1, nil, errors.New("no selector matched")
}

func (p *healthPage) Eval(js string, args ...interface{}) (*browser.EvalResult, error) {
	p.scripts = append(p.scripts, js)
	// botpkg.PageClickAndWaitNavigation: set a window token, then poll for
	// it to be gone (a navigation happened).
	switch {
	case strings.Contains(js, "window[k] = 1"):
		return browser.NewEvalResult(true), nil
	case strings.Contains(js, "window[k] === 1"):
		return browser.NewEvalResult(false), nil
	}
	v, err := p.eval(p, js, args)
	if err != nil {
		return nil, err
	}
	return browser.NewEvalResult(v), nil
}

// healthEl is a fake form control.
type healthEl struct {
	browser.ElementHandle
	page  *healthPage
	name  string
	value string
	// mangle, when set, changes what Input stores (a dropped keystroke).
	mangle func(string) string
}

func (e *healthEl) Input(t string) error {
	if e.mangle != nil {
		t = e.mangle(t)
	}
	e.value = t
	return nil
}
func (e *healthEl) Property(string) (interface{}, error) { return e.value, nil }
func (e *healthEl) Focus() error                         { return nil }
func (e *healthEl) Click() error {
	if e.page.onClick != nil {
		e.page.onClick(e.page, e)
	}
	return nil
}

func (p *healthPage) addEl(sel, name string) *healthEl {
	if p.els == nil {
		p.els = map[string]*healthEl{}
	}
	el := &healthEl{page: p, name: name}
	p.els[sel] = el
	return el
}

func healthFixture(t *testing.T, name string) interface{} {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func healthState(url, user, text string, main bool) map[string]interface{} {
	return map[string]interface{}{"url": url, "user": user, "text": text, "lines": text, "main": main}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestHealthCheckItemID(t *testing.T) {
	for in, want := range map[string]string{"38472619": "38472619", " 1 ": "1", "12345678901234567890": "12345678901234567890"} {
		got, err := checkItemID(in)
		if err != nil || got != want {
			t.Errorf("checkItemID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1.5", "1&x=2", "123456789012345678901", "1 2", jsInjectionPayload} {
		if got, err := checkItemID(in); err == nil {
			t.Errorf("checkItemID(%q) = %q, want error", in, got)
		}
	}
}

func TestHealthURLs(t *testing.T) {
	if got := itemURL("42"); got != "https://news.ycombinator.com/item?id=42" {
		t.Errorf("itemURL = %q", got)
	}
	if got := submittedURL("a b&c"); got != "https://news.ycombinator.com/submitted?id=a+b%26c" {
		t.Errorf("submittedURL = %q", got)
	}
	b := &HackerNewsBot{}
	if b.Platform() != "HACKERNEWS" || b.LoginURL() != "https://news.ycombinator.com/" ||
		b.SearchURL("x") != "https://news.ycombinator.com/" || b.ExtractUsername("https://news.ycombinator.com/user?id=x") != "" {
		t.Error("static adapter values changed")
	}
	if _, err := b.GetProfileData(context.Background(), nil); err == nil {
		t.Error("GetProfileData should be unsupported")
	}
	if err := b.SendMessage(context.Background(), nil, "u", "m"); err == nil {
		t.Error("SendMessage should be unsupported")
	}
}

func TestHealthRefusalOnlyOnBarePages(t *testing.T) {
	cases := []struct {
		st   pageState
		want string
	}{
		{pageState{Main: false, Lines: "Sorry. You're posting too fast. Please slow down."}, "You're posting too fast."},
		{pageState{Main: true, Lines: "You're posting too fast."}, ""}, // a comment quoting it
		{pageState{Main: false, Lines: "Just a page"}, ""},
		{pageState{Main: false, Lines: "Hacker News\nValidation required. Check your email."}, "Validation required."},
	}
	for _, c := range cases {
		if got := c.st.refusal(); got != c.want {
			t.Errorf("refusal(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}

func TestHealthSentenceAtAndClip(t *testing.T) {
	line := "First. Second one here! Third?"
	if got := sentenceAt(line, strings.Index(line, "one")); got != "Second one here!" {
		t.Errorf("sentenceAt = %q", got)
	}
	if got := sentenceAt("no punctuation at all", 3); got != "no punctuation at all" {
		t.Errorf("sentenceAt = %q", got)
	}
	if got := clip("héllo", 10); got != "héllo" {
		t.Errorf("clip short = %q", got)
	}
	if got := clip("héllo wörld", 6); got != "héllo…" {
		t.Errorf("clip long = %q", got)
	}
}

func TestHealthNormTextAndNeedle(t *testing.T) {
	if got := normText("  *Hello*   World\n\n again "); got != "hello world again" {
		t.Errorf("normText = %q", got)
	}
	if got := needle("A *b*\n\nC d"); got != "abcd" {
		t.Errorf("needle = %q", got)
	}
	long := needle(strings.Repeat("ab ", 100))
	if n := len([]rune(long)); n != 80 {
		t.Errorf("needle length = %d, want 80", n)
	}
	if got := needle("ÄÖÜ äöü"); got != "äöüäöü" {
		t.Errorf("needle unicode = %q", got)
	}
}

func TestHealthSameLink(t *testing.T) {
	yes := [][2]string{
		{"https://Example.com/a/", "http://example.com/a"},
		{"https://www.example.com/x", "example.com/x"},
		{" HTTPS://EXAMPLE.COM ", "https://example.com/"},
	}
	for _, c := range yes {
		if !sameLink(c[0], c[1]) {
			t.Errorf("sameLink(%q, %q) = false", c[0], c[1])
		}
	}
	no := [][2]string{{"", ""}, {"https://example.com", ""}, {"https://example.com/a", "https://example.com/b"}, {"https://a.com", "https://b.com"}}
	for _, c := range no {
		if sameLink(c[0], c[1]) {
			t.Errorf("sameLink(%q, %q) = true", c[0], c[1])
		}
	}
}

func TestHealthCommentToMap(t *testing.T) {
	c := Comment{ID: "1", Author: "a", Text: "t", Depth: 2, ParentID: "0", Age: "2026", Deleted: true}
	want := map[string]interface{}{"id": "1", "author": "a", "text": "t", "depth": 2, "parentId": "0", "age": "2026", "deleted": true}
	if got := c.toMap(); !reflect.DeepEqual(got, want) {
		t.Errorf("toMap = %#v", got)
	}
}

func TestHealthIsLoggedIn(t *testing.T) {
	b := &HackerNewsBot{}
	p := &healthPage{}
	if ok, _ := b.IsLoggedIn(p); ok {
		t.Error("logged in without #me")
	}
	p.addEl("#me", "me")
	if ok, err := b.IsLoggedIn(p); !ok || err != nil {
		t.Errorf("IsLoggedIn = %v, %v", ok, err)
	}
}

func TestHealthGetMethodByName(t *testing.T) {
	b := &HackerNewsBot{}
	for _, n := range []string{"submit_post", "reply_to_comment", "list_comments", "get_post_metrics"} {
		fn, ok := b.GetMethodByName(n)
		if !ok || fn == nil {
			t.Fatalf("%s not exposed", n)
		}
		if _, err := fn(context.Background()); err == nil || !strings.Contains(err.Error(), n) {
			t.Errorf("%s without page: err = %v", n, err)
		}
	}
	if _, ok := b.GetMethodByName("nope"); ok {
		t.Error("unknown method exposed")
	}
	// Missing required arguments fail before touching the page.
	p := &healthPage{}
	fn, _ := b.GetMethodByName("reply_to_comment")
	if _, err := fn(context.Background(), p, "1"); err == nil || !strings.Contains(err.Error(), "text") {
		t.Errorf("reply without text: %v", err)
	}
	if len(p.navs) != 0 {
		t.Errorf("navigated: %v", p.navs)
	}
}

// ---------------------------------------------------------------------------
// get_post_metrics
// ---------------------------------------------------------------------------

func metricsPage(t *testing.T, result interface{}, state map[string]interface{}) *healthPage {
	return &healthPage{eval: func(p *healthPage, script string, args []interface{}) (interface{}, error) {
		switch script {
		case postMetricsScript:
			if len(args) != 1 || args[0] != "100" {
				t.Errorf("metrics args = %v", args)
			}
			return result, nil
		case pageStateJS:
			return state, nil
		}
		t.Fatalf("unexpected script %.60q", script)
		return nil, nil
	}}
}

func TestHealthGetPostMetrics(t *testing.T) {
	b := &HackerNewsBot{}
	p := metricsPage(t, healthFixture(t, "health_metrics.json"), nil)
	m, err := b.GetPostMetrics(context.Background(), p, " 100 ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"itemID": "100", "title": "Show HN: A synthetic story", "author": "alice",
		"age": "2026-10-01T09:00:00", "isJob": false, "points": 123, "comments": 45}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("metrics = %#v", m)
	}
	if !reflect.DeepEqual(p.navs, []string{"https://news.ycombinator.com/item?id=100"}) {
		t.Errorf("navs = %v", p.navs)
	}
}

func TestHealthGetPostMetricsJobHasNullCounts(t *testing.T) {
	b := &HackerNewsBot{}
	m, err := b.GetPostMetrics(context.Background(), metricsPage(t, healthFixture(t, "health_metrics_job.json"), nil), "100")
	if err != nil {
		t.Fatal(err)
	}
	if m["points"] != nil || m["comments"] != nil || m["isJob"] != true {
		t.Errorf("job metrics = %#v", m)
	}
	if _, present := m["points"]; !present {
		t.Error("points key missing; want explicit nil")
	}
}

func TestHealthGetPostMetricsNotFoundQuotesPage(t *testing.T) {
	b := &HackerNewsBot{}
	p := metricsPage(t, map[string]interface{}{"found": false}, healthState("https://news.ycombinator.com/item?id=100", "", "No such item.", false))
	_, err := b.GetPostMetrics(context.Background(), p, "100")
	if err == nil || !strings.Contains(err.Error(), "item 100 not found") || !strings.Contains(err.Error(), "No such item.") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthGetPostMetricsRejectsBadIDWithoutNavigating(t *testing.T) {
	b := &HackerNewsBot{}
	p := metricsPage(t, nil, nil)
	if _, err := b.GetPostMetrics(context.Background(), p, jsInjectionPayload); err == nil {
		t.Fatal("want error")
	}
	if len(p.navs) != 0 || len(p.scripts) != 0 {
		t.Errorf("touched the page: navs=%v scripts=%d", p.navs, len(p.scripts))
	}
}

func TestHealthGetPostMetricsEvalError(t *testing.T) {
	b := &HackerNewsBot{}
	p := &healthPage{eval: func(*healthPage, string, []interface{}) (interface{}, error) {
		return nil, errors.New("csp blocked")
	}}
	if _, err := b.GetPostMetrics(context.Background(), p, "1"); err == nil || !strings.Contains(err.Error(), "reading metrics") {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// list_comments
// ---------------------------------------------------------------------------

// commentsPages serves health_comments_p1.json on the item page and
// health_comments_p2.json on its p=2 page, recording the stack argument.
func commentsPages(t *testing.T) (*healthPage, *[]interface{}) {
	p1, p2 := healthFixture(t, "health_comments_p1.json"), healthFixture(t, "health_comments_p2.json")
	var stacks []interface{}
	return &healthPage{eval: func(p *healthPage, script string, args []interface{}) (interface{}, error) {
		if script != commentsJS {
			t.Fatalf("unexpected script %.60q", script)
		}
		if len(args) != 2 || args[0] != "100" {
			t.Errorf("comments args = %v", args)
		}
		stacks = append(stacks, args[1])
		if strings.Contains(p.url, "p=2") {
			return p2, nil
		}
		return p1, nil
	}}, &stacks
}

func TestHealthListCommentsFollowsMoreAndDedupes(t *testing.T) {
	b := &HackerNewsBot{}
	p, stacks := commentsPages(t)
	got, err := b.ListComments(context.Background(), p, "100", false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c["id"].(string))
	}
	if strings.Join(ids, ",") != "101,102,103,104,105" {
		t.Errorf("ids = %v", ids)
	}
	if got[1]["parentId"] != "101" || got[1]["depth"] != 1 || got[1]["text"] != "A reply to alice\n\nwith a second paragraph" {
		t.Errorf("reply = %#v", got[1])
	}
	if got[2]["deleted"] != true {
		t.Errorf("deleted comment = %#v", got[2])
	}
	// p=2 is followed; the foreign "More" link on p=2 is not.
	if !reflect.DeepEqual(p.navs, []string{"https://news.ycombinator.com/item?id=100", "https://news.ycombinator.com/item?id=100&p=2"}) {
		t.Errorf("navs = %v", p.navs)
	}
	// The open-ancestor stack of page 1 is carried into page 2.
	if len(*stacks) != 2 || !reflect.DeepEqual((*stacks)[0], []string(nil)) || !reflect.DeepEqual((*stacks)[1], []string{"103"}) {
		t.Errorf("stacks = %#v", *stacks)
	}
}

func TestHealthListCommentsTopLevelOnly(t *testing.T) {
	b := &HackerNewsBot{}
	p, _ := commentsPages(t)
	got, err := b.ListComments(context.Background(), p, "100", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c["depth"] != 0 {
			t.Errorf("non-top-level comment kept: %#v", c)
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d top-level comments, want 3", len(got))
	}
}

func TestHealthListCommentsItemNotFound(t *testing.T) {
	b := &HackerNewsBot{}
	p := &healthPage{eval: func(p *healthPage, script string, _ []interface{}) (interface{}, error) {
		if script == pageStateJS {
			return healthState(p.url, "", "No such item.", false), nil
		}
		return map[string]interface{}{"comments": []interface{}{}, "found": false}, nil
	}}
	_, err := b.ListComments(context.Background(), p, "100", false)
	if err == nil || !strings.Contains(err.Error(), "item 100 not found") || !strings.Contains(err.Error(), "No such item.") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthListCommentsStopsOnCancelledContext(t *testing.T) {
	b := &HackerNewsBot{}
	p, _ := commentsPages(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.ListComments(ctx, p, "100", false); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if len(p.navs) != 0 {
		t.Errorf("navigated after cancel: %v", p.navs)
	}
}

func TestHealthListCommentsBadID(t *testing.T) {
	b := &HackerNewsBot{}
	if _, err := b.ListComments(context.Background(), &healthPage{}, "item?id=1", false); err == nil {
		t.Error("want error")
	}
}

func TestHealthReadStateError(t *testing.T) {
	p := &healthPage{eval: func(*healthPage, string, []interface{}) (interface{}, error) { return nil, errors.New("boom") }}
	if _, err := readState(p); err == nil || !strings.Contains(err.Error(), "reading page state") {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// submit_post
// ---------------------------------------------------------------------------

// submitSite fakes /submit, /submitted?id=me and the post-submit redirect.
// The submit click lands on afterURL with afterState; once submitted, the
// /submitted page lists newRows after the old ones.
type submitSite struct {
	afterURL   string
	afterState map[string]interface{}
	oldRows    []interface{}
	newRows    []interface{}
	submitted  bool
	submitUser string
}

func (s *submitSite) page(t *testing.T) *healthPage {
	p := &healthPage{}
	p.eval = func(p *healthPage, script string, _ []interface{}) (interface{}, error) {
		switch script {
		case pageStateJS:
			if s.submitted && p.url == s.afterURL {
				return s.afterState, nil
			}
			return healthState(p.url, s.submitUser, "Submit title url text", true), nil
		case submittedRowsJS:
			if s.submitted {
				return append(append([]interface{}{}, s.newRows...), s.oldRows...), nil
			}
			return s.oldRows, nil
		}
		t.Fatalf("unexpected script %.60q", script)
		return nil, nil
	}
	p.addEl("form input[name='title']", "title")
	p.addEl("form input[name='url']", "url")
	p.addEl("form textarea[name='text']", "text")
	p.addEl("form:has(input[name='title']) input[type='submit']", "submit")
	p.onClick = func(p *healthPage, el *healthEl) {
		if el.name == "submit" {
			s.submitted = true
			p.url = s.afterURL
		}
	}
	return p
}

func newSubmitSite() *submitSite {
	return &submitSite{
		submitUser: "me",
		afterURL:   "https://news.ycombinator.com/newest",
		afterState: healthState("https://news.ycombinator.com/newest", "me", "New links", true),
		oldRows:    []interface{}{map[string]interface{}{"id": "900", "title": "Older story", "link": "https://old.example/"}},
		newRows: []interface{}{
			map[string]interface{}{"id": "not-a-number", "title": "Show HN: Health", "link": ""},
			map[string]interface{}{"id": "901", "title": "Show HN: *Health*", "link": "https://www.example.com/x/"},
		},
	}
}

func TestHealthSubmitPostConfirmedOnSubmitted(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	p := s.page(t)
	b := &HackerNewsBot{}
	res, err := b.SubmitPost(context.Background(), p, " Show HN: Health ", "https://example.com/x", "Some *body* text")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"id": "901", "title": "Show HN: *Health*", "url": "https://news.ycombinator.com/item?id=901",
		"link": "https://www.example.com/x/", "author": "me"}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("res = %#v", res)
	}
	if p.els["form input[name='title']"].value != "Show HN: Health" || p.els["form input[name='url']"].value != "https://example.com/x" ||
		p.els["form textarea[name='text']"].value != "Some *body* text" {
		t.Error("form fields not typed as given")
	}
}

func TestHealthSubmitPostMatchesByLinkWhenTitleDiffers(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	s.newRows = []interface{}{map[string]interface{}{"id": "902", "title": "HN-edited title", "link": "http://example.com/x"}}
	res, err := (&HackerNewsBot{}).SubmitPost(context.Background(), s.page(t), "My title", "https://example.com/x/", "")
	if err != nil || res["id"] != "902" {
		t.Errorf("res = %v, err = %v", res, err)
	}
}

func TestHealthSubmitPostInputValidation(t *testing.T) {
	b := &HackerNewsBot{}
	p := &healthPage{}
	for _, c := range []struct{ title, link string }{
		{"  ", ""},
		{"t", "ftp://example.com/"},
		{"t", "example.com/no-scheme"},
		{"t", "https://"},
	} {
		if _, err := b.SubmitPost(context.Background(), p, c.title, c.link, ""); err == nil {
			t.Errorf("SubmitPost(%q, %q): want error", c.title, c.link)
		}
	}
	if len(p.navs) != 0 {
		t.Errorf("navigated: %v", p.navs)
	}
}

func TestHealthSubmitPostLoggedOut(t *testing.T) {
	s := newSubmitSite()
	s.submitUser = ""
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), s.page(t), "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthSubmitPostBareRefusalPage(t *testing.T) {
	p := &healthPage{eval: func(p *healthPage, script string, _ []interface{}) (interface{}, error) {
		return healthState(p.url, "", "You have to be logged in to submit.\nLogin\nusername:", false), nil
	}}
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), p, "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "submit page refused: You have to be logged in to submit.") || strings.Contains(err.Error(), "username") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthSubmitPostDuplicateRedirect(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	s.afterURL = "https://news.ycombinator.com/item?id=777"
	s.afterState = healthState(s.afterURL, "me", "Existing story", true)
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), s.page(t), "t", "https://example.com/", "")
	if err == nil || !strings.Contains(err.Error(), "already submitted") || !strings.Contains(err.Error(), "777") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthSubmitPostRateLimited(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	s.afterURL = "https://news.ycombinator.com/r"
	s.afterState = healthState(s.afterURL, "", "Sorry. You're posting too fast. Please slow down.", false)
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), s.page(t), "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "refused the submission: You're posting too fast.") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthSubmitPostRejectedFormShownAgain(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	s.afterURL = "https://news.ycombinator.com/submit"
	s.afterState = healthState(s.afterURL, "me", "That title is too long.", true)
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), s.page(t), "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "rejected the submission: That title is too long.") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthSubmitPostNotConfirmedDoesNotResubmit(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	s.newRows = nil
	p := s.page(t)
	clicks := 0
	inner := p.onClick
	p.onClick = func(p *healthPage, el *healthEl) {
		if el.name == "submit" {
			clicks++
		}
		inner(p, el)
	}
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), p, "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "not resubmitting") || !strings.Contains(err.Error(), "/submitted?id=me") {
		t.Errorf("err = %v", err)
	}
	if clicks != 1 {
		t.Errorf("submit clicked %d times, want 1", clicks)
	}
}

func TestHealthSubmitPostDroppedKeystrokeIsNotSubmitted(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	p := s.page(t)
	p.els["form input[name='title']"].mangle = func(v string) string { return v[:len(v)-1] }
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), p, "Title", "", "")
	if err == nil || !strings.Contains(err.Error(), "not submitting") {
		t.Errorf("err = %v", err)
	}
	if s.submitted {
		t.Error("form was submitted with a wrong title")
	}
}

func TestHealthSubmitPostMissingTitleField(t *testing.T) {
	fastConfirm(t)
	s := newSubmitSite()
	p := s.page(t)
	delete(p.els, "form input[name='title']")
	_, err := (&HackerNewsBot{}).SubmitPost(context.Background(), p, "t", "", "")
	if err == nil || !strings.Contains(err.Error(), "title field not found") {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// reply_to_comment
// ---------------------------------------------------------------------------

func replyPage(t *testing.T, user string, foundID string) *healthPage {
	p := &healthPage{}
	p.eval = func(p *healthPage, script string, args []interface{}) (interface{}, error) {
		switch script {
		case pageStateJS:
			return healthState(p.url, user, "reply page", true), nil
		case findMyCommentJS:
			if len(args) != 3 || args[0] != user || args[1] != "helloworld" || args[2] != "555" {
				t.Errorf("findMyComment args = %v", args)
			}
			return map[string]interface{}{"id": foundID}, nil
		}
		t.Fatalf("unexpected script %.60q", script)
		return nil, nil
	}
	p.addEl("form[action='comment'] textarea[name='text']", "text")
	p.addEl("form[action='comment'] input[type='submit']", "submit")
	p.onClick = func(p *healthPage, el *healthEl) { p.url = "https://news.ycombinator.com/item?id=555" }
	return p
}

func TestHealthReplyToCommentConfirmed(t *testing.T) {
	fastConfirm(t)
	p := replyPage(t, "me", "556")
	res, err := (&HackerNewsBot{}).ReplyToComment(context.Background(), p, "555", "Hello *world*")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"success": true, "itemID": "555", "commentID": "556", "url": "https://news.ycombinator.com/item?id=556"}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("res = %#v", res)
	}
	if len(p.navs) != 1 || p.navs[0] != buildReplyURL("555") {
		t.Errorf("navs = %v", p.navs)
	}
}

func TestHealthReplyToCommentNotConfirmed(t *testing.T) {
	fastConfirm(t)
	confirmTimeout = 50 * time.Millisecond
	_, err := (&HackerNewsBot{}).ReplyToComment(context.Background(), replyPage(t, "me", ""), "555", "hello world")
	if err == nil || !strings.Contains(err.Error(), "reply not confirmed") {
		t.Errorf("err = %v", err)
	}
}

func TestHealthReplyToCommentLoggedOutAndValidation(t *testing.T) {
	b := &HackerNewsBot{}
	if _, err := b.ReplyToComment(context.Background(), &healthPage{}, "555", "  "); err == nil {
		t.Error("empty reply accepted")
	}
	if _, err := b.ReplyToComment(context.Background(), &healthPage{}, "x", "hi"); err == nil {
		t.Error("bad id accepted")
	}
	_, err := b.ReplyToComment(context.Background(), replyPage(t, "", ""), "555", "hi")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("err = %v", err)
	}
}
