// Package browserctltest is a fake bridge for tests of code built on
// browserctl: a browser with tabs that answers the bridge commands
// browserctl sends, records every command, and lets a test answer any
// command type itself.
package browserctltest

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
)

// Handler answers one command: its data, or an error (success:false).
type Handler func(cmd *extension.Command) (any, error)

// Fake is a browserctl.Sender with one browser.
type Fake struct {
	mu       sync.Mutex
	Instance string
	Label    string
	tabs     map[int]*browserctl.Tab
	nextTab  int
	handlers map[string]Handler
	calls    []extension.Command
}

var _ browserctl.Sender = (*Fake)(nil)

// New is a fake browser "inst-1" with no tabs.
func New() *Fake {
	return &Fake{Instance: "inst-1", Label: "Test", tabs: map[int]*browserctl.Tab{}, nextTab: 100, handlers: map[string]Handler{}}
}

// AddTab adds a tab and returns its id.
func (f *Fake) AddTab(url, group string, active bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextTab++
	f.tabs[f.nextTab] = &browserctl.Tab{TabID: f.nextTab, WindowID: 1, URL: url, Title: url, GroupTitle: group, Active: active}
	return f.nextTab
}

// SetURL changes a tab's URL (a page navigated by itself).
func (f *Fake) SetURL(tabID int, url string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.tabs[tabID]; t != nil {
		t.URL = url
	}
}

// Tab returns a copy of a tab, ok false when it is not open.
func (f *Fake) Tab(tabID int) (browserctl.Tab, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tabs[tabID]
	if t == nil {
		return browserctl.Tab{}, false
	}
	return *t, true
}

// On makes h answer every command of type cmdType.
func (f *Fake) On(cmdType string, h Handler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[cmdType] = h
}

// Calls returns the commands sent so far.
func (f *Fake) Calls() []extension.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]extension.Command(nil), f.calls...)
}

// Types returns the types of the commands sent so far.
func (f *Fake) Types() []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, c.Type)
	}
	return out
}

func (f *Fake) ResolveTarget(t extension.Target) (extension.ConnInfo, error) {
	if t.Instance != "" && t.Instance != f.Instance {
		return extension.ConnInfo{}, extension.ErrBrowserGone
	}
	return extension.ConnInfo{Instance: f.Instance, Label: f.Label, ConnectedAt: time.Unix(0, 0)}, nil
}

func (f *Fake) Browsers() ([]extension.ConnInfo, error) {
	info, _ := f.ResolveTarget(extension.Target{})
	return []extension.ConnInfo{info}, nil
}

func (f *Fake) SendCommandTo(t extension.Target, cmd *extension.Command, _ time.Duration) (*extension.Response, error) {
	if t.Instance != "" && t.Instance != f.Instance {
		return nil, extension.ErrBrowserGone
	}
	f.mu.Lock()
	f.calls = append(f.calls, *cmd)
	h := f.handlers[cmd.Type]
	if h == nil && cmd.Type == extension.CmdCdp {
		h = f.cdp(f.handlers["eval_cdp"])
	}
	f.mu.Unlock()
	var data any
	var err error
	if h != nil {
		data, err = h(cmd)
	} else {
		data, err = f.builtin(cmd)
	}
	if err != nil {
		return &extension.Response{ID: cmd.ID, Error: err.Error()}, fmt.Errorf("extension error: %s", err)
	}
	return &extension.Response{ID: cmd.ID, Success: true, Data: data}, nil
}

// builtin answers the tab commands from the fake's tabs; anything else
// succeeds with {ok:true}.
func (f *Fake) builtin(cmd *extension.Command) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab := f.tabs[cmd.TabID]
	needTab := func() error {
		if tab == nil {
			return fmt.Errorf("No tab with id: %d.", cmd.TabID)
		}
		return nil
	}
	str := func(k string) string { s, _ := cmd.Params[k].(string); return s }
	switch cmd.Type {
	case extension.CmdListTabs:
		ids := make([]int, 0, len(f.tabs))
		for id := range f.tabs {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		tabs := []browserctl.Tab{}
		for _, id := range ids {
			tabs = append(tabs, *f.tabs[id])
		}
		return map[string]any{"tabs": tabs}, nil
	case extension.CmdPageInfo:
		if err := needTab(); err != nil {
			return nil, err
		}
		return map[string]any{"tabId": tab.TabID, "url": tab.URL, "title": tab.Title, "status": "complete"}, nil
	case extension.CmdCreateTab:
		f.nextTab++
		for _, t := range f.tabs {
			t.Active = false
		}
		f.tabs[f.nextTab] = &browserctl.Tab{TabID: f.nextTab, WindowID: 1, URL: str("url"), Title: str("url"), Active: true}
		return map[string]any{"tabId": f.nextTab, "url": str("url")}, nil
	case extension.CmdNavigate:
		if err := needTab(); err != nil {
			return nil, err
		}
		tab.URL = str("url")
		return map[string]any{"tabId": tab.TabID, "url": tab.URL}, nil
	case extension.CmdCloseTab:
		if err := needTab(); err != nil {
			return nil, err
		}
		delete(f.tabs, cmd.TabID)
		return map[string]any{"tabId": cmd.TabID}, nil
	case extension.CmdTabGroup:
		if err := needTab(); err != nil {
			return nil, err
		}
		tab.GroupTitle, tab.GroupID = str("title"), 7
		return map[string]any{"groupId": 7}, nil
	case extension.CmdWatchTab:
		if err := needTab(); err != nil {
			return nil, err
		}
		return map[string]any{"watching": cmd.Params["on"]}, nil
	case extension.CmdSubmitGuard:
		return map[string]any{"armed": true, "blocked": []any{}}, nil
	case extension.CmdElement:
		if err := needTab(); err != nil {
			return nil, err
		}
		return map[string]any{"elementId": "el:" + str("selector")}, nil
	}
	return map[string]any{"ok": true}, nil
}

// cdp answers the raw CDP relay the way the gate's isolated-world scripts
// use it: a main frame, a new world, and Runtime.evaluate answered by the
// eval_cdp handler (eval) with the expression, as an eval_cdp would be.
func (f *Fake) cdp(eval Handler) Handler {
	return func(cmd *extension.Command) (any, error) {
		method, _ := cmd.Params["method"].(string)
		params, _ := cmd.Params["params"].(map[string]interface{})
		switch method {
		case "Page.getFrameTree":
			return map[string]any{"result": map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main"}}}}, nil
		case "Page.createIsolatedWorld":
			return map[string]any{"result": map[string]any{"executionContextId": 7}}, nil
		case "Runtime.evaluate":
			var value any
			if eval != nil {
				data, err := eval(&extension.Command{ID: cmd.ID, Type: "eval_cdp", TabID: cmd.TabID,
					Params: map[string]interface{}{"expression": params["expression"]}})
				if err != nil {
					return nil, err
				}
				if m, ok := data.(map[string]any); ok {
					value = m["result"]
				}
			}
			return map[string]any{"result": map[string]any{"result": map[string]any{"type": "object", "value": value}}}, nil
		}
		return map[string]any{"result": map[string]any{}}, nil
	}
}
