package workflow

import (
	"context"
	"html"
	"strings"
)

// ReplySender delivers a node's message carrying reply's links (as text,
// MessageText, or HTML, MessageHTML, or the app's own buttons from
// reply.Links).
type ReplySender func(ctx context.Context, reply WaitReply) error

// SendAndWait is n8n's sendAndWait for a node of any app: on the node's
// first run it opens a reply, sends the message through send and pauses;
// once answered it outputs {data: {approved}} / {data: {text}} /
// {data: {<field>...}} (each with respondedAt). Past spec.Limit the input
// items continue unchanged, as in n8n. A failed send fails the node and
// drops the reply, so a retry sends a new one; a re-run before the answer
// never sends twice.
func SendAndWait(ctx context.Context, input NodeInput, spec WaitReplySpec, send ReplySender) ([]NodeOutput, error) {
	return sendAndWait(ctx, input, spec, send, (*WaitReplyResult).Items)
}

// WaitForCall is n8n's dispatch-and-wait for a node that hands a callback
// URL to another system (a GitHub workflow dispatch): on the node's first
// run it opens a webhook reply on the execution's resume URL
// ($execution.resumeUrl), calls send with it (reply.URL) and pauses until
// the URL is called; spec.HTTPMethod and spec.Limit apply. The reply is
// open before send, so a callback that comes back at once is not lost.
// Once called, the output is the call's JSON body, as n8n's resume webhook
// outputs it: an object is one item, a list one item per element. Past the
// limit the input items continue.
func WaitForCall(ctx context.Context, input NodeInput, spec WaitReplySpec, send ReplySender) ([]NodeOutput, error) {
	spec.Kind, spec.ExecutionURL = WaitReplyWebhook, true
	return sendAndWait(ctx, input, spec, send, func(r *WaitReplyResult) []Item { return callBodyItems(r.Answer["body"]) })
}

// callBodyItems turns a call's decoded body into items.
func callBodyItems(body interface{}) []Item {
	list, ok := body.([]interface{})
	if !ok {
		list = []interface{}{body}
	}
	items := make([]Item, 0, len(list))
	for _, v := range list {
		switch m := v.(type) {
		case map[string]interface{}:
			items = append(items, Item{JSON: m})
		case nil:
			items = append(items, Item{JSON: map[string]interface{}{}})
		default:
			items = append(items, Item{JSON: map[string]interface{}{"data": m}})
		}
	}
	return items
}

func sendAndWait(ctx context.Context, input NodeInput, spec WaitReplySpec, send ReplySender, answered func(*WaitReplyResult) []Item) ([]NodeOutput, error) {
	res, err := WaitReplyAnswer(ctx, input)
	if err != nil {
		return nil, err
	}
	if res != nil {
		switch {
		case res.Status == WaitReplyExpired:
			return []NodeOutput{{Handle: "main", Items: input.Items}}, nil
		case res.Status == WaitReplyAnswered:
			return []NodeOutput{{Handle: "main", Items: answered(res)}}, nil
		case res.Sent:
			return nil, res.Pause()
		}
	}
	// No reply yet, or one whose message never went out (the process
	// stopped in between): NewWaitReply returns the open one then.
	reply, err := NewWaitReply(ctx, input, spec)
	if err != nil {
		return nil, err
	}
	if err := send(ctx, reply); err != nil {
		_ = DiscardWaitReply(ctx, input)
		return nil, err
	}
	if err := MarkWaitReplySent(ctx, input); err != nil {
		return nil, err
	}
	return nil, reply.Pause()
}

// MessageText is message followed by the reply's links, one per line
// ("Approve: https://…"), for plain-text channels.
func (r WaitReply) MessageText(message string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(message, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	for i, l := range r.Links {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l.Label + ": " + l.URL)
	}
	return b.String()
}

// MessageHTML is message (escaped, line breaks kept) followed by the
// reply's links as buttons, for email.
func (r WaitReply) MessageHTML(message string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:sans-serif">`)
	if message != "" {
		b.WriteString(`<p>` + strings.ReplaceAll(html.EscapeString(message), "\n", "<br>") + `</p>`)
	}
	b.WriteString(`<p>`)
	for _, l := range r.Links {
		style := "display:inline-block;margin:4px 8px 4px 0;padding:10px 18px;border-radius:6px;text-decoration:none;"
		if l.Primary {
			style += "background:#1d1d1b;color:#ffffff;border:1px solid #1d1d1b"
		} else {
			style += "background:#ffffff;color:#1d1d1b;border:1px solid #1d1d1b"
		}
		b.WriteString(`<a href="` + html.EscapeString(l.URL) + `" style="` + style + `">` + html.EscapeString(l.Label) + `</a>`)
	}
	b.WriteString(`</p></div>`)
	return b.String()
}
