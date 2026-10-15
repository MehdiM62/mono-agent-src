package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// RespondChatNode is n8n's Chat node ("Respond to Chat"): it sends a
// message into the chat session of the trigger.chat that started the run
// and, with operation sendAndWait, waits for the person's answer there.
//
// Config fields (n8n's names):
//
//	"operation"       send (default) or sendAndWait
//	"message"         the text
//	"responseType"    sendAndWait: freeTextChat (default; the next message
//	                  typed is the answer) or approval (buttons; a typed
//	                  message counts as disapproval)
//	"approvalOptions" {values: {approvalType single|double, approveLabel,
//	                  disapproveLabel}}
//	"blockUserInput"  approval: no typing until a button is pressed
//	"options"         {limitWaitTime: {values: {limitType, resumeAmount,
//	                  resumeUnit, maxDateAndTime}}}
//	"nestUnderData"   true for n8n's version 1.1 output shape
//
// Outputs: send — the input items. sendAndWait — a typed answer as
// {sessionId, action, chatInput} (approval adds data: {approved: false});
// a button as {data: {approved}}; past the limit, the input items.
type RespondChatNode struct{}

func (n *RespondChatNode) Type() string { return "core.respond_chat" }

func (n *RespondChatNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	session, _ := input.Trigger["sessionId"].(string)
	if session == "" {
		return nil, errors.New("respond to chat: the run was not started by a chat trigger (trigger.chat): there is no chat session to answer")
	}
	message, _ := config["message"].(string)
	pass := []workflow.NodeOutput{{Handle: "main", Items: input.Items}}
	switch op, _ := config["operation"].(string); op {
	case "", "send":
		workflow.PostChatMessage(session, workflow.ChatMessage{Role: "bot", Text: message})
		return pass, nil
	case "sendAndWait":
	default:
		return nil, fmt.Errorf("%w: respond to chat \"operation\" must be send or sendAndWait, got %q", workflow.ErrInvalidConfig, op)
	}

	res, err := workflow.WaitReplyAnswer(ctx, input)
	if err != nil {
		return nil, err
	}
	if res != nil {
		switch {
		case res.Status == workflow.WaitReplyExpired:
			return pass, nil
		case res.Status == workflow.WaitReplyAnswered:
			nest, _ := config["nestUnderData"].(bool)
			return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{chatAnswerItem(res, nest)}}}, nil
		case res.Sent:
			return nil, res.Pause()
		}
	}
	spec, block, err := chatReplySpec(config, session, time.Now())
	if err != nil {
		return nil, err
	}
	reply, err := workflow.NewWaitReply(ctx, input, spec)
	if err != nil {
		return nil, err
	}
	msg := workflow.ChatMessage{Role: "bot", Text: message}
	if spec.Kind == workflow.WaitReplyApproval {
		// The primary (approve) button first, as n8n's chat shows them.
		msg.Buttons = slices.Clone(reply.Links)
		slices.Reverse(msg.Buttons)
		msg.BlockInput = block
	}
	workflow.PostChatMessage(session, msg)
	if err := workflow.MarkWaitReplySent(ctx, input); err != nil {
		return nil, err
	}
	return nil, reply.Pause()
}

// chatReplySpec builds the reply a sendAndWait opens in session.
func chatReplySpec(config map[string]interface{}, session string, now time.Time) (workflow.WaitReplySpec, bool, error) {
	spec := workflow.WaitReplySpec{Kind: workflow.WaitReplyFreeText, ChatSession: session}
	block := false
	switch rt, _ := config["responseType"].(string); rt {
	case "", "freeTextChat", "freeText":
	case "approval":
		spec.Kind = workflow.WaitReplyApproval
		ao := jsonObject(config["approvalOptions"])
		v, _ := ao["values"].(map[string]interface{})
		spec.ApprovalType, _ = v["approvalType"].(string)
		spec.ApproveLabel, _ = v["approveLabel"].(string)
		spec.DisapproveLabel, _ = v["disapproveLabel"].(string)
		block, _ = config["blockUserInput"].(bool)
	default:
		return spec, false, fmt.Errorf("%w: respond to chat \"responseType\" must be freeTextChat or approval, got %q", workflow.ErrInvalidConfig, rt)
	}
	opts := jsonObject(config["options"])
	if lim, ok := opts["limitWaitTime"].(map[string]interface{}); ok {
		values, _ := lim["values"].(map[string]interface{})
		if values == nil {
			values = lim
		}
		t, err := workflow.ParseReplyLimit(values, now)
		if err != nil {
			return spec, false, fmt.Errorf("respond to chat: %w", err)
		}
		spec.Limit = t
	}
	return spec, block, nil
}

// chatAnswerItem is the answer as n8n's Chat node outputs it.
func chatAnswerItem(res *workflow.WaitReplyResult, nest bool) workflow.Item {
	if _, typed := res.Answer["chatInput"]; !typed {
		return res.Items()[0] // a button: {data: {approved}}
	}
	j := make(map[string]interface{}, len(res.Answer)+1)
	for k, v := range res.Answer {
		j[k] = v
	}
	if nest {
		if res.Kind == workflow.WaitReplyApproval {
			j["approved"] = false
		}
		return workflow.Item{JSON: map[string]interface{}{"data": j}}
	}
	if res.Kind == workflow.WaitReplyApproval {
		j["data"] = map[string]interface{}{"approved": false}
	}
	return workflow.Item{JSON: j}
}

// jsonObject reads an object config value, given as an object or as its
// JSON text (the editor's code fields).
func jsonObject(v interface{}) map[string]interface{} {
	if s, ok := v.(string); ok {
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(s), &m)
		return m
	}
	m, _ := v.(map[string]interface{})
	return m
}
