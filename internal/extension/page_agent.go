package extension

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The coder's browser commands (plan C1). Each helper sends one command and
// decodes its data into the struct that mirrors it field for field; the
// extension's half is chrome-extension/agent_*.js.

// agentSlowTimeout bounds the commands that can take a while on a big page:
// a full-page screenshot, the accessibility tree.
const agentSlowTimeout = 60 * time.Second

// agentTabsTimeout bounds list_tabs and tab_group, which only ask Chrome.
const agentTabsTimeout = 10 * time.Second

// Submit guard actions (submit_guard's params.action).
const (
	SubmitGuardArm       = "arm"
	SubmitGuardDisarm    = "disarm"
	SubmitGuardAllowOnce = "allow_once"
	SubmitGuardStatus    = "status"
)

// TabInfo is one tab of list_tabs. GroupID is -1 and GroupTitle empty for a
// tab in no group.
type TabInfo struct {
	TabID      int    `json:"tabId"`
	WindowID   int    `json:"windowId"`
	GroupID    int    `json:"groupId"`
	GroupTitle string `json:"groupTitle"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Active     bool   `json:"active"`
}

// ScreenshotOptions are screenshot's params. Mode is "viewport" (the
// extension's default when empty), "full", "element" (Selector) or "clip"
// (Clip, CSS pixels in document coordinates). Format is "png" (default) or
// "jpeg", with Quality 0-100.
type ScreenshotOptions struct {
	Mode     string `json:"mode,omitempty"`
	Selector string `json:"selector,omitempty"`
	Clip     *Clip  `json:"clip,omitempty"`
	Format   string `json:"format,omitempty"`
	Quality  int    `json:"quality,omitempty"`
}

// Clip is a rectangle of the page in CSS pixels.
type Clip struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Screenshot is screenshot's data: the image in base64, its type and its
// size in image pixels.
type Screenshot struct {
	Data   string `json:"data"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Bytes decodes the image.
func (s *Screenshot) Bytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(s.Data)
}

// EmulateOptions are emulate's params. Any of Width, Height,
// DeviceScaleFactor and Mobile sets the device metrics override to exactly
// those values; Media sets the emulated media features (an empty value
// clears that one). Reset clears both. What is not named is left as it is.
type EmulateOptions struct {
	Width             int            `json:"width,omitempty"`
	Height            int            `json:"height,omitempty"`
	DeviceScaleFactor float64        `json:"deviceScaleFactor,omitempty"`
	Mobile            bool           `json:"mobile,omitempty"`
	Media             *EmulatedMedia `json:"media,omitempty"`
	Reset             bool           `json:"reset,omitempty"`
}

// EmulatedMedia: PrefersColorScheme "light" | "dark", PrefersReducedMotion
// "reduce" | "no-preference".
type EmulatedMedia struct {
	PrefersColorScheme   string `json:"prefersColorScheme,omitempty"`
	PrefersReducedMotion string `json:"prefersReducedMotion,omitempty"`
}

// ConsoleQuery are read_console's params: entries after Since (a seq), at
// most Limit, of the named levels (debug, log, info, warning, error; all
// when empty).
type ConsoleQuery struct {
	Since  int64    `json:"since,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Levels []string `json:"levels,omitempty"`
}

// ConsoleEntry is one console message, uncaught exception or browser log
// entry. Line and Column are 1-based; Stack is set for errors that carry one.
type ConsoleEntry struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Level  string `json:"level"`
	Text   string `json:"text"`
	URL    string `json:"url"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Stack  string `json:"stack,omitempty"`
}

// ConsolePage is read_console's data. Next is the Since of the next read.
type ConsolePage struct {
	Entries []ConsoleEntry `json:"entries"`
	Next    int64          `json:"next"`
}

// NetworkQuery are read_network's params. FailedOnly keeps requests that
// failed and HTTP 4xx/5xx answers.
type NetworkQuery struct {
	Since      int64 `json:"since,omitempty"`
	Limit      int   `json:"limit,omitempty"`
	FailedOnly bool  `json:"failedOnly,omitempty"`
}

// NetworkEntry is one finished or failed request; a request still in flight
// is not listed yet. Never headers or bodies.
type NetworkEntry struct {
	Seq        int64  `json:"seq"`
	TS         int64  `json:"ts"`
	RequestID  string `json:"requestId"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	MIMEType   string `json:"mimeType"`
	Type       string `json:"type"`
	Failed     bool   `json:"failed"`
	ErrorText  string `json:"errorText"`
	DurationMs int64  `json:"durationMs"`
	Size       int64  `json:"size"`
}

// NetworkPage is read_network's data. Next is the Since of the next read.
type NetworkPage struct {
	Entries []NetworkEntry `json:"entries"`
	Next    int64          `json:"next"`
}

// A11yNode is one node of the accessibility tree, ignored nodes pruned.
type A11yNode struct {
	Role     string     `json:"role"`
	Name     string     `json:"name"`
	Value    string     `json:"value,omitempty"`
	Children []A11yNode `json:"children,omitempty"`
}

// A11yTreeResult is a11y_tree's data. Truncated says the tree was cut at
// the extension's node cap.
type A11yTreeResult struct {
	Nodes     []A11yNode `json:"nodes"`
	Truncated bool       `json:"truncated,omitempty"`
}

// SubmitGuardState is submit_guard's data: whether the guard is armed and
// the submits it has blocked on this tab, oldest first.
type SubmitGuardState struct {
	Armed   bool            `json:"armed"`
	Blocked []BlockedSubmit `json:"blocked"`
}

// BlockedSubmit is one submit the guard stopped.
type BlockedSubmit struct {
	Seq  int64      `json:"seq"`
	TS   int64      `json:"ts"`
	URL  string     `json:"url"`
	Form SubmitForm `json:"form"`
}

// SubmitForm summarises the stopped form. Password, hidden and card-like
// values arrive masked as "•••". Submitter is a CSS selector for the control
// that submitted it ("" when none is known); Selector is one for the form.
type SubmitForm struct {
	Action    string        `json:"action"`
	Method    string        `json:"method"`
	Fields    []SubmitField `json:"fields"`
	Submitter string        `json:"submitter"`
	Selector  string        `json:"selector,omitempty"`
}

// SubmitField is one field of a SubmitForm.
type SubmitField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// SubmitPromptResult is submit_prompt's data.
type SubmitPromptResult struct {
	Shown          bool   `json:"shown"`
	SubmitSelector string `json:"submitSelector,omitempty"`
}

// ListTabs lists every tab of the browser t resolves to.
func (r *RemoteSender) ListTabs(t Target) ([]TabInfo, error) {
	resp, err := r.SendCommandTo(t, &Command{Type: CmdListTabs}, agentTabsTimeout)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tabs []TabInfo `json:"tabs"`
	}
	if err := decodeData(resp, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", CmdListTabs, err)
	}
	return out.Tabs, nil
}

// TabGroup puts tabID into the group titled title in the tab's window,
// creating it there when the window has none, and returns the group's id.
// color is one of Chrome's group colors (grey, blue, red, yellow, green,
// pink, purple, cyan, orange) or "" for Chrome's choice.
func (r *RemoteSender) TabGroup(t Target, tabID int, title, color string) (int, error) {
	params := map[string]interface{}{"title": title}
	if color != "" {
		params["color"] = color
	}
	resp, err := r.SendCommandTo(t, &Command{Type: CmdTabGroup, TabID: tabID, Params: params}, agentTabsTimeout)
	if err != nil {
		return 0, err
	}
	var out struct {
		GroupID *int `json:"groupId"`
	}
	if err := decodeData(resp, &out); err != nil {
		return 0, fmt.Errorf("%s: %w", CmdTabGroup, err)
	}
	if out.GroupID == nil {
		return 0, fmt.Errorf("%s: response missing groupId", CmdTabGroup)
	}
	return *out.GroupID, nil
}

// PageFor is a page handle on tabID in the browser t resolves to. A page
// from NewExtensionPage(r, tabID) always goes to the default browser.
func (r *RemoteSender) PageFor(t Target, tabID int) *ExtensionPage {
	return NewExtensionPage(pinnedSender{via: r, t: t}, tabID)
}

// Screenshot captures this tab.
func (ep *ExtensionPage) Screenshot(o ScreenshotOptions) (*Screenshot, error) {
	var out Screenshot
	if err := ep.agentCall(CmdScreenshot, o, agentSlowTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Emulate changes the viewport or media features this tab is rendered with.
// It lasts until Reset, or until the person cancels Chrome's debugging
// banner or the tab closes.
func (ep *ExtensionPage) Emulate(o EmulateOptions) error {
	return ep.agentCall(CmdEmulate, o, 0, nil)
}

// WatchTab starts (on) or stops buffering this tab's console and network
// activity, and reports whether it is now watched.
func (ep *ExtensionPage) WatchTab(on bool) (bool, error) {
	var out struct {
		Watching bool `json:"watching"`
	}
	if err := ep.agentCall(CmdWatchTab, map[string]interface{}{"on": on}, 0, &out); err != nil {
		return false, err
	}
	return out.Watching, nil
}

// ReadConsole reads this tab's buffered console entries (WatchTab first).
// The buffers live in the extension's service worker: a restart of it clears
// them, and the next read fails saying the tab is not watched.
func (ep *ExtensionPage) ReadConsole(q ConsoleQuery) (*ConsolePage, error) {
	var out ConsolePage
	if err := ep.agentCall(CmdReadConsole, q, 0, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadNetwork reads this tab's buffered requests (WatchTab first).
func (ep *ExtensionPage) ReadNetwork(q NetworkQuery) (*NetworkPage, error) {
	var out NetworkPage
	if err := ep.agentCall(CmdReadNetwork, q, 0, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// A11yTree reads the accessibility tree of the page, or of the first element
// matching selector, down to depth levels (0: no limit).
func (ep *ExtensionPage) A11yTree(selector string, depth int) (*A11yTreeResult, error) {
	params := map[string]interface{}{}
	if selector != "" {
		params["selector"] = selector
	}
	if depth > 0 {
		params["depth"] = depth
	}
	var out A11yTreeResult
	if err := ep.agentCall(CmdA11yTree, params, agentSlowTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitGuard arms, disarms, lets one submit through (allow_once) or reports
// (status) this tab's submit guard; see the SubmitGuard* actions.
func (ep *ExtensionPage) SubmitGuard(action string) (*SubmitGuardState, error) {
	var out SubmitGuardState
	if err := ep.agentCall(CmdSubmitGuard, map[string]interface{}{"action": action}, 0, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitPrompt shows the in-page banner asking the person to review the form
// and press Submit themselves, outlining the submit control of the form at
// selector (or of the page's form when empty). It never submits. An empty
// message shows the default one.
func (ep *ExtensionPage) SubmitPrompt(selector, message string) (*SubmitPromptResult, error) {
	params := map[string]interface{}{}
	if selector != "" {
		params["selector"] = selector
	}
	if message != "" {
		params["message"] = message
	}
	var out SubmitPromptResult
	if err := ep.agentCall(CmdSubmitPrompt, params, 0, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// agentCall sends cmdType with params (a map, or a struct that marshals to
// one) and decodes the data into out when out is not nil. min raises the
// page's own timeout for a command that can be slow.
func (ep *ExtensionPage) agentCall(cmdType string, params any, min time.Duration, out any) error {
	m, err := paramsOf(params)
	if err != nil {
		return fmt.Errorf("%s: %w", cmdType, err)
	}
	timeout := ep.effectiveTimeout()
	if timeout < min {
		timeout = min
	}
	resp, err := ep.server.SendCommand(&Command{Type: cmdType, TabID: ep.tabID, Params: m}, timeout)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := decodeData(resp, out); err != nil {
		return fmt.Errorf("%s: %w", cmdType, err)
	}
	return nil
}

// paramsOf turns a params struct into the command's params map.
func paramsOf(v any) (map[string]interface{}, error) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}
	blob, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// decodeData reads a response's data into out.
func decodeData(resp *Response, out any) error {
	if resp == nil || resp.Data == nil {
		return errors.New("empty response")
	}
	blob, err := json.Marshal(resp.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(blob, out)
}
