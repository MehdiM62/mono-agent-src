package browserctl

import (
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
)

// Bridge command types this package sends that the extension added for it
// (C1). The older ones are extension.Cmd* constants.
const (
	CmdListTabs     = "list_tabs"
	CmdTabGroup     = "tab_group"
	CmdScreenshot   = "screenshot"
	CmdEmulate      = "emulate"
	CmdWatchTab     = "watch_tab"
	CmdReadConsole  = "read_console"
	CmdReadNetwork  = "read_network"
	CmdA11yTree     = "a11y_tree"
	CmdSubmitGuard  = "submit_guard"
	CmdSubmitPrompt = "submit_prompt"
)

// loadTimeout bounds opening or navigating a tab (the extension waits for
// the page to load).
const loadTimeout = 60 * time.Second

// Tab is one open tab as list_tabs reports it.
type Tab struct {
	TabID      int    `json:"tabId"`
	WindowID   int    `json:"windowId"`
	GroupID    int    `json:"groupId"`
	GroupTitle string `json:"groupTitle"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Active     bool   `json:"active"`
}

// ListTabs lists the browser's tabs, unfiltered.
func (s *Session) ListTabs() ([]Tab, error) {
	var out struct {
		Tabs []Tab `json:"tabs"`
	}
	if err := s.call(0, CmdListTabs, nil, DefaultTimeout, &out); err != nil {
		return nil, err
	}
	if out.Tabs == nil {
		out.Tabs = []Tab{}
	}
	return out.Tabs, nil
}

// VisibleTabs is ListTabs as the caller may see it: everything for a
// human; for an agent, its own group's tabs and tabs on allowed pages.
func (s *Session) VisibleTabs() ([]Tab, error) {
	tabs, err := s.ListTabs()
	if err != nil || !s.policy.Agent {
		return tabs, err
	}
	group := GroupTitle()
	out := []Tab{}
	for _, t := range tabs {
		if t.GroupTitle == group || s.policy.Allowed(t.URL) {
			out = append(out, t)
		}
	}
	return out, nil
}

// OpenTab opens url in a new tab and makes it the session's tab. An
// agent's tab goes into its group (group, else GroupTitle) and is watched
// from the start: the page is reloaded once the watch is on, so errors
// raised while it loads are kept. A human's tab is grouped only when group
// is given.
func (s *Session) OpenTab(url, group string) (int, error) {
	if err := s.policy.Check(url); err != nil {
		return 0, err
	}
	var created struct {
		TabID int `json:"tabId"`
	}
	if err := s.call(0, extension.CmdCreateTab, map[string]interface{}{"url": url}, loadTimeout, &created); err != nil {
		return 0, err
	}
	if created.TabID == 0 {
		return 0, fmt.Errorf("create_tab: the extension answered without a tab id")
	}
	s.tabID = created.TabID
	if group == "" && s.policy.Agent {
		group = GroupTitle()
	}
	if group != "" {
		if err := s.call(s.tabID, CmdTabGroup, map[string]interface{}{"title": group}, DefaultTimeout, nil); err != nil {
			return s.tabID, fmt.Errorf("tab %d opened, but grouping it failed: %w", s.tabID, err)
		}
	}
	if s.policy.Agent {
		if err := s.Watch(true); err != nil {
			return s.tabID, fmt.Errorf("tab %d opened, but watching its console and network failed: %w", s.tabID, err)
		}
		if _, err := s.send(s.tabID, extension.CmdReload, nil, loadTimeout); err != nil {
			return s.tabID, err
		}
	}
	// The page may have redirected while it loaded.
	if err := s.UseTab(s.tabID); err != nil {
		return s.tabID, err
	}
	return s.tabID, nil
}

// Navigate loads url in the session's tab and returns the URL it ended on.
// An agent's tab is watched first, so the new page's errors are kept.
func (s *Session) Navigate(url string) (string, error) {
	if err := s.policy.Check(url); err != nil {
		return "", err
	}
	if s.policy.Agent && !blank(s.url) {
		if err := s.Watch(true); err != nil {
			return "", err
		}
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := s.Call(extension.CmdNavigate, map[string]interface{}{"url": url}, loadTimeout, &out); err != nil {
		return "", err
	}
	if s.policy.Agent && blank(s.url) {
		if err := s.Watch(true); err != nil {
			return "", err
		}
	}
	if err := s.UseTab(s.tabID); err != nil {
		return out.URL, err
	}
	return s.url, nil
}

// Reload reloads the tab; hard bypasses the cache.
func (s *Session) Reload(hard bool) error {
	if !hard {
		_, err := s.Send(extension.CmdReload, nil, loadTimeout)
		return err
	}
	if _, err := s.Page().CDP("Page.reload", map[string]interface{}{"ignoreCache": true}); err != nil {
		return err
	}
	// Page.reload returns before the tab starts loading; until then
	// wait_load would see the old page complete.
	time.Sleep(250 * time.Millisecond)
	_, err := s.Send(extension.CmdWaitLoad, map[string]interface{}{"timeout": loadTimeout.Milliseconds()}, loadTimeout+5*time.Second)
	return err
}

// CloseTab closes the session's tab and forgets it as the context's last.
func (s *Session) CloseTab() error {
	if _, err := s.Send(extension.CmdCloseTab, nil, DefaultTimeout); err != nil {
		return err
	}
	if s.state.TabID == s.tabID {
		s.state.TabID = 0
		delete(s.state.Reported, tabKey(s.info.Instance, s.tabID))
		_ = s.saveState()
	}
	s.tabID, s.url = 0, ""
	return nil
}

// Watch turns the tab's console and network buffering on or off.
func (s *Session) Watch(on bool) error {
	_, err := s.Send(CmdWatchTab, map[string]interface{}{"on": on}, DefaultTimeout)
	return err
}

// ConsoleEntry is one read_console entry.
type ConsoleEntry struct {
	Seq    int     `json:"seq"`
	TS     float64 `json:"ts"`
	Level  string  `json:"level"`
	Text   string  `json:"text"`
	URL    string  `json:"url,omitempty"`
	Line   int     `json:"line,omitempty"`
	Column int     `json:"column,omitempty"`
	Stack  string  `json:"stack,omitempty"`
}

// NetworkEntry is one read_network entry (never headers or bodies).
type NetworkEntry struct {
	Seq        int     `json:"seq"`
	TS         float64 `json:"ts"`
	RequestID  string  `json:"requestId"`
	Method     string  `json:"method"`
	URL        string  `json:"url"`
	Status     int     `json:"status"`
	StatusText string  `json:"statusText,omitempty"`
	MimeType   string  `json:"mimeType,omitempty"`
	Type       string  `json:"type,omitempty"`
	Failed     bool    `json:"failed"`
	ErrorText  string  `json:"errorText,omitempty"`
	DurationMs float64 `json:"durationMs,omitempty"`
	Size       float64 `json:"size,omitempty"`
}

// Console reads the tab's buffered console entries after seq since,
// optionally only levels. The watch is turned on first (it is idempotent).
func (s *Session) Console(since int, levels []string, limit int) ([]ConsoleEntry, int, error) {
	if err := s.Watch(true); err != nil {
		return nil, 0, err
	}
	params := map[string]interface{}{"since": since}
	if len(levels) > 0 {
		params["levels"] = levels
	}
	if limit > 0 {
		params["limit"] = limit
	}
	var out struct {
		Entries []ConsoleEntry `json:"entries"`
		Next    int            `json:"next"`
	}
	if err := s.Call(CmdReadConsole, params, DefaultTimeout, &out); err != nil {
		return nil, 0, err
	}
	if out.Entries == nil {
		out.Entries = []ConsoleEntry{}
	}
	return out.Entries, out.Next, nil
}

// Network reads the tab's buffered requests after seq since.
func (s *Session) Network(since int, failedOnly bool, limit int) ([]NetworkEntry, int, error) {
	if err := s.Watch(true); err != nil {
		return nil, 0, err
	}
	params := map[string]interface{}{"since": since, "failedOnly": failedOnly}
	if limit > 0 {
		params["limit"] = limit
	}
	var out struct {
		Entries []NetworkEntry `json:"entries"`
		Next    int            `json:"next"`
	}
	if err := s.Call(CmdReadNetwork, params, DefaultTimeout, &out); err != nil {
		return nil, 0, err
	}
	if out.Entries == nil {
		out.Entries = []NetworkEntry{}
	}
	return out.Entries, out.Next, nil
}
