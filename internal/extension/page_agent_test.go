package extension

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The helpers send the C1 command type to their own tab with the params as
// the contract names them.
func TestAgentHelpersSendTheContractsParams(t *testing.T) {
	ok := &Response{Success: true, Data: map[string]interface{}{}}
	for _, tc := range []struct {
		name string
		call func(ep *ExtensionPage)
		typ  string
		want map[string]interface{}
	}{
		{"screenshot", func(ep *ExtensionPage) {
			_, _ = ep.Screenshot(ScreenshotOptions{Mode: "clip", Clip: &Clip{X: 1, Y: 2, Width: 30, Height: 40}, Format: "jpeg", Quality: 70})
		}, CmdScreenshot, map[string]interface{}{"mode": "clip", "clip": map[string]interface{}{"x": 1.0, "y": 2.0, "width": 30.0, "height": 40.0}, "format": "jpeg", "quality": 70.0}},
		{"screenshot defaults", func(ep *ExtensionPage) { _, _ = ep.Screenshot(ScreenshotOptions{}) }, CmdScreenshot, map[string]interface{}{}},
		{"emulate", func(ep *ExtensionPage) {
			_ = ep.Emulate(EmulateOptions{Width: 375, Height: 812, DeviceScaleFactor: 3, Mobile: true, Media: &EmulatedMedia{PrefersColorScheme: "dark"}})
		}, CmdEmulate, map[string]interface{}{"width": 375.0, "height": 812.0, "deviceScaleFactor": 3.0, "mobile": true, "media": map[string]interface{}{"prefersColorScheme": "dark"}}},
		{"emulate reset", func(ep *ExtensionPage) { _ = ep.Emulate(EmulateOptions{Reset: true}) }, CmdEmulate, map[string]interface{}{"reset": true}},
		{"watch on", func(ep *ExtensionPage) { _, _ = ep.WatchTab(true) }, CmdWatchTab, map[string]interface{}{"on": true}},
		{"watch off", func(ep *ExtensionPage) { _, _ = ep.WatchTab(false) }, CmdWatchTab, map[string]interface{}{"on": false}},
		{"console", func(ep *ExtensionPage) {
			_, _ = ep.ReadConsole(ConsoleQuery{Since: 12, Limit: 50, Levels: []string{"error", "warning"}})
		}, CmdReadConsole, map[string]interface{}{"since": 12.0, "limit": 50.0, "levels": []interface{}{"error", "warning"}}},
		{"network", func(ep *ExtensionPage) { _, _ = ep.ReadNetwork(NetworkQuery{Since: 3, FailedOnly: true}) }, CmdReadNetwork, map[string]interface{}{"since": 3.0, "failedOnly": true}},
		{"a11y", func(ep *ExtensionPage) { _, _ = ep.A11yTree("main", 4) }, CmdA11yTree, map[string]interface{}{"selector": "main", "depth": 4}},
		{"a11y whole page", func(ep *ExtensionPage) { _, _ = ep.A11yTree("", 0) }, CmdA11yTree, map[string]interface{}{}},
		{"guard", func(ep *ExtensionPage) { _, _ = ep.SubmitGuard(SubmitGuardAllowOnce) }, CmdSubmitGuard, map[string]interface{}{"action": "allow_once"}},
		{"prompt", func(ep *ExtensionPage) { _, _ = ep.SubmitPrompt("#signup", "") }, CmdSubmitPrompt, map[string]interface{}{"selector": "#signup"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeSender{resp: ok}
			tc.call(NewExtensionPage(s, 42))
			if len(s.cmds) != 1 {
				t.Fatalf("sent %d commands", len(s.cmds))
			}
			cmd := s.cmds[0]
			if cmd.Type != tc.typ || cmd.TabID != 42 {
				t.Fatalf("sent %s to tab %d, want %s to 42", cmd.Type, cmd.TabID, tc.typ)
			}
			if !reflect.DeepEqual(cmd.Params, tc.want) {
				t.Fatalf("params = %#v\nwant     %#v", cmd.Params, tc.want)
			}
		})
	}
}

// The data decodes into the typed results, field for field.
func TestAgentHelpersDecodeTheContractsData(t *testing.T) {
	data := func(js string) *fakeSender {
		var d interface{}
		if err := json.Unmarshal([]byte(js), &d); err != nil {
			t.Fatal(err)
		}
		return &fakeSender{resp: &Response{Success: true, Data: d}}
	}

	shot, err := NewExtensionPage(data(`{"data":"iVBORw==","mime":"image/png","width":800,"height":600}`), 1).Screenshot(ScreenshotOptions{})
	if err != nil || shot.MIME != "image/png" || shot.Width != 800 || shot.Height != 600 {
		t.Fatalf("shot = %+v err = %v", shot, err)
	}
	if b, err := shot.Bytes(); err != nil || len(b) != 4 || b[1] != 'P' {
		t.Fatalf("bytes = %q err = %v", b, err)
	}

	cons, err := NewExtensionPage(data(`{"entries":[{"seq":7,"ts":1760000000000,"level":"error","text":"boom","url":"http://localhost:5173/app.js","line":3,"column":9,"stack":"at f (app.js:3:9)"}],"next":7}`), 1).ReadConsole(ConsoleQuery{})
	want := ConsoleEntry{Seq: 7, TS: 1760000000000, Level: "error", Text: "boom", URL: "http://localhost:5173/app.js", Line: 3, Column: 9, Stack: "at f (app.js:3:9)"}
	if err != nil || len(cons.Entries) != 1 || cons.Entries[0] != want || cons.Next != 7 {
		t.Fatalf("console = %+v err = %v", cons, err)
	}

	net, err := NewExtensionPage(data(`{"entries":[{"seq":2,"ts":5,"requestId":"9.1","method":"GET","url":"http://localhost/x","status":404,"statusText":"Not Found","mimeType":"text/html","type":"Fetch","failed":false,"errorText":"","durationMs":12,"size":321}],"next":2}`), 1).ReadNetwork(NetworkQuery{})
	wantNet := NetworkEntry{Seq: 2, TS: 5, RequestID: "9.1", Method: "GET", URL: "http://localhost/x", Status: 404, StatusText: "Not Found", MIMEType: "text/html", Type: "Fetch", DurationMs: 12, Size: 321}
	if err != nil || len(net.Entries) != 1 || net.Entries[0] != wantNet || net.Next != 2 {
		t.Fatalf("network = %+v err = %v", net, err)
	}

	tree, err := NewExtensionPage(data(`{"nodes":[{"role":"main","name":"","children":[{"role":"button","name":"Save"},{"role":"textbox","name":"Email","value":"a@b.c"}]}]}`), 1).A11yTree("", 0)
	if err != nil || len(tree.Nodes) != 1 || len(tree.Nodes[0].Children) != 2 || tree.Nodes[0].Children[1].Value != "a@b.c" || tree.Truncated {
		t.Fatalf("tree = %+v err = %v", tree, err)
	}

	guard, err := NewExtensionPage(data(`{"armed":true,"blocked":[{"seq":1,"ts":9,"url":"https://shop.test/cart","form":{"action":"https://shop.test/pay","method":"POST","fields":[{"name":"card","type":"text","label":"Card","value":"•••"}],"submitter":"#pay","selector":"form#checkout"}}]}`), 1).SubmitGuard(SubmitGuardStatus)
	if err != nil || !guard.Armed || len(guard.Blocked) != 1 {
		t.Fatalf("guard = %+v err = %v", guard, err)
	}
	if f := guard.Blocked[0].Form; f.Method != "POST" || f.Submitter != "#pay" || f.Selector != "form#checkout" || f.Fields[0].Value != "•••" {
		t.Fatalf("form = %+v", f)
	}

	prompt, err := NewExtensionPage(data(`{"shown":true,"submitSelector":"button[type=submit]"}`), 1).SubmitPrompt("", "")
	if err != nil || !prompt.Shown || prompt.SubmitSelector != "button[type=submit]" {
		t.Fatalf("prompt = %+v err = %v", prompt, err)
	}

	if on, err := NewExtensionPage(data(`{"watching":true}`), 1).WatchTab(true); err != nil || !on {
		t.Fatalf("watching = %v err = %v", on, err)
	}
}

func TestAgentHelpersSurfaceTheExtensionsError(t *testing.T) {
	s := &fakeSender{resp: &Response{Success: false, Error: "tab 5 is not watched: send watch_tab first"}}
	if _, err := NewExtensionPage(s, 5).ReadConsole(ConsoleQuery{}); err == nil || !strings.Contains(err.Error(), "not watched") {
		t.Fatalf("err = %v", err)
	}
	empty := &fakeSender{resp: &Response{Success: true}}
	if _, err := NewExtensionPage(empty, 5).Screenshot(ScreenshotOptions{}); err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("err = %v, want an empty-response error", err)
	}
}

// targetRelay answers /monoagent/relay with data, recording each request's
// query and command.
func targetRelay(t *testing.T, data interface{}) (*RemoteSender, func() (url.Values, Command)) {
	t.Helper()
	var mu sync.Mutex
	var q url.Values
	var got Command
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		q = r.URL.Query()
		_ = json.NewDecoder(r.Body).Decode(&got)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(Response{ID: got.ID, Success: true, Data: data})
	}))
	t.Cleanup(srv.Close)
	return &RemoteSender{baseURL: srv.URL, client: &http.Client{}}, func() (url.Values, Command) {
		mu.Lock()
		defer mu.Unlock()
		return q, got
	}
}

func TestListTabsGoesToTheTargetBrowser(t *testing.T) {
	r, seen := targetRelay(t, map[string]interface{}{"tabs": []interface{}{
		map[string]interface{}{"tabId": 3, "windowId": 1, "groupId": 8, "groupTitle": "Coder · app", "url": "http://localhost:3000/", "title": "App", "active": true},
		map[string]interface{}{"tabId": 4, "windowId": 1, "groupId": -1, "groupTitle": "", "url": "https://example.com/", "title": "Example", "active": false},
	}})
	tabs, err := r.ListTabs(Target{Instance: "edge-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []TabInfo{
		{TabID: 3, WindowID: 1, GroupID: 8, GroupTitle: "Coder · app", URL: "http://localhost:3000/", Title: "App", Active: true},
		{TabID: 4, WindowID: 1, GroupID: -1, URL: "https://example.com/", Title: "Example"},
	}
	if !reflect.DeepEqual(tabs, want) {
		t.Fatalf("tabs = %+v", tabs)
	}
	q, cmd := seen()
	if q.Get("instance") != "edge-1" || cmd.Type != CmdListTabs {
		t.Fatalf("query %v, command %+v", q, cmd)
	}
}

func TestTabGroupSendsTitleAndColor(t *testing.T) {
	r, seen := targetRelay(t, map[string]interface{}{"groupId": 0})
	id, err := r.TabGroup(Target{Profile: "work"}, 12, "Coder · app", "cyan")
	if err != nil || id != 0 {
		t.Fatalf("id = %d err = %v (group 0 is a real group id)", id, err)
	}
	q, cmd := seen()
	if q.Get("profile") != "work" || cmd.Type != CmdTabGroup || cmd.TabID != 12 ||
		cmd.Params["title"] != "Coder · app" || cmd.Params["color"] != "cyan" {
		t.Fatalf("query %v, command %+v", q, cmd)
	}

	r, _ = targetRelay(t, map[string]interface{}{})
	if _, err := r.TabGroup(Target{}, 12, "x", ""); err == nil || !strings.Contains(err.Error(), "missing groupId") {
		t.Fatalf("err = %v, want missing groupId", err)
	}
}

func TestTabIDIsThePagesTab(t *testing.T) {
	if id := NewExtensionPage(&fakeSender{}, 42).TabID(); id != 42 {
		t.Fatalf("TabID = %d, want 42", id)
	}
}

func TestPageForPinsTheTarget(t *testing.T) {
	r, seen := targetRelay(t, map[string]interface{}{"watching": true})
	if _, err := r.PageFor(Target{Instance: "chrome-2"}, 9).WatchTab(true); err != nil {
		t.Fatal(err)
	}
	q, cmd := seen()
	if q.Get("instance") != "chrome-2" || cmd.Type != CmdWatchTab || cmd.TabID != 9 {
		t.Fatalf("query %v, command %+v", q, cmd)
	}
}

func TestSubmitGuardAllowOnceNamesTheFrame(t *testing.T) {
	ok := &Response{Success: true, Data: map[string]interface{}{"armed": true, "origin": "https://shop.test", "blocked": []interface{}{
		map[string]interface{}{"seq": 3, "ts": 1, "url": "https://shop.test/pay", "frameId": 7, "form": map[string]interface{}{"selector": "#pay"}},
	}}}
	for _, tc := range []struct {
		frame    int
		selector string
		want     map[string]interface{}
	}{
		{7, "", map[string]interface{}{"action": "allow_once", "frameId": 7}},
		{0, "", map[string]interface{}{"action": "allow_once", "frameId": 0}},
		{-1, "#pay", map[string]interface{}{"action": "allow_once", "selector": "#pay"}},
		{-1, "", map[string]interface{}{"action": "allow_once"}},
	} {
		s := &fakeSender{resp: ok}
		st, err := NewExtensionPage(s, 4).SubmitGuardAllowOnce(tc.frame, tc.selector)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.cmds[0].Params, tc.want) {
			t.Fatalf("params = %#v, want %#v", s.cmds[0].Params, tc.want)
		}
		if st.Origin != "https://shop.test" || st.Blocked[0].FrameID != 7 {
			t.Fatalf("state = %+v", st)
		}
	}
}

// WithExpectOrigin puts expectOrigin on every command of the page and of
// the elements it finds, and leaves the caller's params alone.
func TestWithExpectOriginRidesOnEveryCommand(t *testing.T) {
	s := &fakeSender{resp: &Response{Success: true, Data: map[string]interface{}{"elementId": "el_1", "watching": true}}}
	p := NewExtensionPage(s, 9).WithExpectOrigin("http://localhost:5173")
	if p.TabID() != 9 {
		t.Fatalf("tab = %d", p.TabID())
	}
	if _, err := p.WatchTab(true); err != nil {
		t.Fatal(err)
	}
	el, err := p.Element("#save", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = el.Click()
	if len(s.cmds) != 3 {
		t.Fatalf("sent %d commands", len(s.cmds))
	}
	for _, c := range s.cmds {
		if c.Params["expectOrigin"] != "http://localhost:5173" || c.TabID != 9 {
			t.Fatalf("%s params = %#v", c.Type, c.Params)
		}
	}
	params := map[string]interface{}{"on": true}
	_ = p.agentCall(CmdWatchTab, params, 0, nil)
	if _, leaked := params["expectOrigin"]; leaked {
		t.Fatal("the caller's params map was changed")
	}
	plain := &fakeSender{resp: s.resp}
	_, _ = NewExtensionPage(plain, 9).WatchTab(true)
	if _, set := plain.cmds[0].Params["expectOrigin"]; set {
		t.Fatal("a page without WithExpectOrigin sent expectOrigin")
	}
}
