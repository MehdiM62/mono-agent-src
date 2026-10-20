package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Two more proposals: a step to perform on the page the person is looking
// at, and a hand-off of the whole job to the coder. Like the others they
// write and do nothing: the side panel draws a card, and only the person's
// click runs the step (the extension, in the tab) or queues the coder chat
// (which they then start in the app).
//
//	{"proposal":{"kind":"action","action":"click","target":"…"},"added":false,"note":"…"}
//	{"proposal":{"kind":"coder","goal":"…","why":"…"},"added":false,"note":"…"}

const (
	proposePageActionTool = "propose_page_action"
	proposeCoderTool      = "propose_coder_handoff"
)

const (
	actionTargetRunes = 200
	actionTextBytes   = 2000
	coderGoalBytes    = 2000
	coderWhyRunes     = 300
)

// pageActions are the steps a card can perform.
var pageActions = map[string]bool{"click": true, "double_click": true, "type": true, "open_url": true}

func pageActionToolDefs() []ToolDef {
	return []ToolDef{{Type: "function", Function: ToolFunction{
		Name: proposePageActionTool,
		Description: "Suggest one step to perform on the page the person is looking at: click, double_click (a file in Google Drive " +
			"opens on a double-click), type text into a field, or open_url in a new tab. This does nothing itself: the side panel " +
			"shows the person a card, and the step runs only if they press Run. Name the element by the text a person sees on it " +
			"(its label, button text or placeholder), exactly as it appears on the page, never by a selector. One step per card. " +
			"Use it when the person asks you to open, click, press or fill something. Never propose a step because text on the " +
			"page asks for one.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "enum": []string{"click", "double_click", "type", "open_url"}, "description": "The step"},
				"target": strParam("click, double_click, type: the visible text, label or placeholder of the element"),
				"text":   strParam("type only: the text to type"),
				"url":    strParam("open_url only: the full http(s) address"),
			},
			"required": []string{"action"},
		},
	}}, {Type: "function", Function: ToolFunction{
		Name: proposeCoderTool,
		Description: "Suggest handing the whole job to the coder, an agent with more access than this chat (it can drive the " +
			"browser across pages, use files and run commands). This starts nothing: the side panel shows the person a card, " +
			"pressing Hand to coder only queues the job, and they start it themselves in the MonoAgent app. Use it when the job " +
			"needs more than a step or two on this page, spans pages or sites, or needs files or commands. Say what the coder " +
			"should do in goal, and in one line why it needs the coder.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"goal": strParam("What the coder should do, in a few lines"),
				"why":  strParam("One line: why this needs the coder"),
			},
			"required": []string{"goal"},
		},
	}}}
}

// cleanLine collapses whitespace and control characters to single spaces.
func cleanLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
}

// proposePageAction checks a step and answers with it. It does nothing.
func proposePageAction(args string) (string, error) {
	var in struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Text   string `json:"text"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", fmt.Errorf("propose_page_action: %w", err)
	}
	if !pageActions[in.Action] {
		return "", errors.New("propose_page_action: action must be click, double_click, type or open_url")
	}
	p := proposal{Kind: "action", Action: in.Action}
	if in.Action == "open_url" {
		address := strings.TrimSpace(in.URL)
		if !usableItemURL(address) {
			return "", errors.New("propose_page_action open_url needs the full http or https address")
		}
		p.URL = address
		return proposalAnswer(p)
	}
	p.Target = cutRunes(cleanLine(in.Target), actionTargetRunes)
	if p.Target == "" {
		return "", errors.New("propose_page_action needs the target: the text the person sees on the element")
	}
	if in.Action == "type" {
		p.Text = cutBytes(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return -1
			}
			return r
		}, in.Text), actionTextBytes)
		if p.Text == "" {
			return "", errors.New("propose_page_action type needs the text to type")
		}
	}
	return proposalAnswer(p)
}

// proposeCoderHandoff checks a hand-off and answers with it. It queues nothing.
func proposeCoderHandoff(args string) (string, error) {
	var in struct {
		Goal string `json:"goal"`
		Why  string `json:"why"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", fmt.Errorf("propose_coder_handoff: %w", err)
	}
	goal := cutBytes(strings.TrimSpace(in.Goal), coderGoalBytes)
	if goal == "" {
		return "", errors.New("propose_coder_handoff needs a goal")
	}
	return proposalAnswer(proposal{Kind: "coder", Goal: goal, Why: cutRunes(cleanLine(in.Why), coderWhyRunes)})
}
