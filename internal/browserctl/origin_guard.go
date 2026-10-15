package browserctl

import (
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
)

// Between the policy check and a command reaching the tab, the tab can
// move: a redirect, a script, the user. For an agent, every command to its
// tab carries expectOrigin, the origin the check passed, and the extension
// refuses the command (error code origin_changed) when the tab is elsewhere
// by then. A tab with no page yet (""/about:blank) is not allowed for
// anything but navigating or closing it: an about:blank tab can run as the
// page that opened it.

// ExpectOriginParam is the command parameter the extension checks.
const ExpectOriginParam = "expectOrigin"

// OriginChangedCode is the extension's error code for a tab that is no
// longer on the expected origin.
const OriginChangedCode = "origin_changed"

// blankWait bounds waiting for a blank tab to commit its page.
const blankWait = 5 * time.Second

// unguarded commands read where the tab is; they carry no expectOrigin.
var unguarded = map[string]bool{extension.CmdPageInfo: true, extension.CmdWaitLoad: true}

// blankOK are the commands an agent may send a tab with no page yet.
var blankOK = map[string]bool{extension.CmdNavigate: true, extension.CmdCloseTab: true}

// BlankTabError is an agent's command to a tab that has no page yet.
type BlankTabError struct {
	TabID   int
	Profile string
}

func (e *BlankTabError) Error() string {
	return fmt.Sprintf("tab %d has no page yet: open one with `monoagentcli %s browser navigate <url>` first", e.TabID, ProfileFlag(e.Profile))
}

// guardCmd prepares an agent's command to its own tab: refused on a blank
// tab unless it navigates or closes it, else stamped with expectOrigin.
func (s *Session) guardCmd(cmd *extension.Command) error {
	if !s.policy.Agent || cmd.TabID == 0 || cmd.TabID != s.tabID || unguarded[cmd.Type] {
		return nil
	}
	if s.blankTab {
		if blankOK[cmd.Type] {
			return nil
		}
		return &BlankTabError{TabID: s.tabID, Profile: s.profile}
	}
	if s.url == "" {
		return nil
	}
	if cmd.Params == nil {
		cmd.Params = map[string]interface{}{}
	}
	cmd.Params[ExpectOriginParam] = Origin(s.url)
	return nil
}

// sendRaw sends without guardCmd: cleanup after a refusal.
func (s *Session) sendRaw(tabID int, cmdType string, params map[string]interface{}, timeout time.Duration) (*extension.Response, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	return s.sender.SendCommandTo(s.target, &extension.Command{Type: cmdType, TabID: tabID, Params: params}, timeout)
}

// checkTab reads where tab id is and applies the policy. An agent's blank
// tab is given blankWait to commit its page first; one that stays blank is
// returned with blank set.
func (s *Session) checkTab(id int) (u string, isBlank bool, err error) {
	info, err := s.pageInfo(id)
	if err != nil {
		return "", false, err
	}
	if s.policy.Agent && blank(info.URL) {
		_, _ = s.sendRaw(id, extension.CmdWaitLoad, map[string]interface{}{"timeout": blankWait.Milliseconds()}, blankWait+5*time.Second)
		if info, err = s.pageInfo(id); err != nil {
			return "", false, err
		}
	}
	if blank(info.URL) {
		return info.URL, s.policy.Agent, nil
	}
	return info.URL, false, s.policy.Check(info.URL)
}

// dropRefused cleans up a tab an agent's command took to a page it may not
// drive: closed when it is in the agent's group (the agent opened it), else
// left as it is with its console and network watch turned off.
func (s *Session) dropRefused(id int) {
	if tabs, err := s.ListTabs(); err == nil {
		for _, t := range tabs {
			if t.TabID == id && t.GroupTitle == GroupTitle() {
				_, _ = s.sendRaw(id, extension.CmdCloseTab, nil, DefaultTimeout)
				s.forget(id)
				return
			}
		}
	}
	_, _ = s.sendRaw(id, extension.CmdWatchTab, map[string]interface{}{"on": false}, DefaultTimeout)
}

// forget drops tab id from the session and its state.
func (s *Session) forget(id int) {
	if s.state.TabID == id {
		s.state.TabID = 0
	}
	key := tabKey(s.info.Instance, id)
	for k := range s.state.Reported {
		if k == key || strings.HasPrefix(k, key+"|") {
			delete(s.state.Reported, k)
		}
	}
	delete(s.state.Loads, key)
	_ = s.saveState()
	if s.tabID == id {
		s.tabID, s.url, s.blankTab = 0, "", false
	}
}
