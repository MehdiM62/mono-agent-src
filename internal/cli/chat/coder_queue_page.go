package chat

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// The page a queued chat is about: its tab's address and title as the
// browser reports them (`browser tabs`, read from chrome.tabs), never as the
// page's own script would: a page can rewrite JSON.stringify or what its
// scripts see, but not what the extension reads from chrome.tabs. So the
// origin, which picks the project folder, is the browser's. A page can still
// change its own path (history.replaceState) and its title, so the path stays
// percent-encoded, both are one capped line, and the prompt shows them only
// as quoted page data.

const (
	queueMaxPath   = 512
	queueMaxOrigin = 300
)

// queueTab is one `browser tabs --json` row.
type queueTab struct {
	TabID  int    `json:"tabId"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// pickQueueTab finds tab in `browser tabs` output, or with tab 0 the first
// active one.
func pickQueueTab(out []byte, tab int) (queueTab, error) {
	var res struct {
		Tabs []queueTab `json:"tabs"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return queueTab{}, fmt.Errorf("browser tabs returned unreadable output")
	}
	for _, t := range res.Tabs {
		if (tab > 0 && t.TabID == tab) || (tab == 0 && t.Active) {
			return t, nil
		}
	}
	if tab > 0 {
		return queueTab{}, clicore.NotFound("tab %d is not open in that browser (see `monoagentcli browser tabs`)", tab)
	}
	return queueTab{}, clicore.NotFound("the browser has no active tab: pass --tab (see `monoagentcli browser tabs`)")
}

// pageOf splits a tab's address into the item's origin and path. The path is
// the URL's percent-encoded form, so it never holds a newline or any other
// control character.
func pageOf(raw string) (origin, path string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "", "", fmt.Errorf("the tab's address is not a URL: %q", cleanLine(raw, 200))
	}
	origin = cleanLine(u.Scheme+"://"+u.Host, queueMaxOrigin)
	path = cleanLine(u.EscapedPath(), queueMaxPath)
	if path == "" {
		path = "/"
	}
	return origin, path, nil
}

// cleanLine is s as one line: format characters (bidi embeddings,
// overrides and isolates, LRM/RLM/ALM, zero-width spaces and joiners, BOM:
// Unicode category Cf) are dropped, so a page can't visually reorder the
// prompt or the card; every run of spaces and control characters becomes
// one space; and at most max runes are kept.
func cleanLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

// cleanTitle is a page title as one short line.
func cleanTitle(s string) string { return cleanLine(s, queueMaxTitle) }

// fenced quotes lines as a fenced text block whose fence is longer than any
// run of backticks inside, so nothing in them can close it.
func fenced(lines ...string) string {
	longest, run := 0, 0
	for _, l := range lines {
		for _, r := range l {
			if r == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
		run = 0
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + strings.Join(lines, "\n") + "\n" + fence + "\n"
}
