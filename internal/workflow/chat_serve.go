package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TriggerNodeTypeChat is the chat trigger (n8n's Chat Trigger, hosted
// chat): the webhook server serves a chat page at /chat/<path>; each message
// starts a run with {sessionId, action: "sendMessage", chatInput}, unless a
// core.respond_chat in that session waits for the person's reply, which the
// message then answers.
const TriggerNodeTypeChat = "trigger.chat"

// Chat response modes (trigger.chat responseMode, n8n's names).
const (
	// ChatRespondLastNode answers each message with the output of the node
	// that ran last (its "output", "text" or the item as JSON).
	ChatRespondLastNode = "lastNode"
	// ChatRespondNodes leaves the answers to core.respond_chat nodes.
	ChatRespondNodes = "responseNodes"
)

// chatPollWait is how long a page's request for new messages is held when
// there are none.
const chatPollWait = 25 * time.Second

// chatWatchLimit bounds how long a chat watches a run (watchChatRun), unless
// the workflow's executionTimeout is longer: that timeout counts only the
// run's active time, so it bounds no watch by itself.
var chatWatchLimit = 30 * time.Minute

// chatWatchSlowest is the longest pause between the watch's reads of a run
// that waits only for its sub-workflows; the pause doubles from
// webhookPollInterval up to it.
const chatWatchSlowest = time.Second

// chatStillRunning is the chat's answer when the watch gives up on a run.
const chatStillRunning = "Sorry, the workflow is still running, and this chat has stopped waiting for its answer."

// chatRoute is one active trigger.chat node.
type chatRoute struct {
	WorkflowID, NodeID, Path string
	Title, Subtitle          string
	Placeholder              string
	InitialMessages          []string
	ResponseMode             string
	TriggerFn                func(items []Item) // when the server has no runner
}

// chatRoutes are the server's chats, by path.
type chatRoutes struct {
	mu sync.RWMutex
	m  map[string]*chatRoute
}

// parseChatRoute reads a trigger.chat config.
func parseChatRoute(cfg map[string]interface{}) (*chatRoute, error) {
	str := func(k string) string { s, _ := cfg[k].(string); return strings.TrimSpace(s) }
	route := &chatRoute{Path: strings.Trim(str("path"), "/"), Title: str("title"), Subtitle: str("subtitle"),
		Placeholder: str("inputPlaceholder"), ResponseMode: str("responseMode")}
	if route.Path == "" {
		return nil, fmt.Errorf("trigger.chat: missing or invalid \"path\" field in config")
	}
	switch route.ResponseMode {
	case "":
		route.ResponseMode = ChatRespondLastNode
	case ChatRespondLastNode, ChatRespondNodes:
	default:
		return nil, fmt.Errorf("trigger.chat: responseMode %q is not one of lastNode, responseNodes", route.ResponseMode)
	}
	switch v := cfg["initialMessages"].(type) {
	case string:
		for _, line := range strings.Split(v, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				route.InitialMessages = append(route.InitialMessages, line)
			}
		}
	case []interface{}:
		for _, m := range v {
			if s, ok := m.(string); ok && strings.TrimSpace(s) != "" {
				route.InitialMessages = append(route.InitialMessages, s)
			}
		}
	}
	return route, nil
}

// RegisterChat serves route at /chat/<path>. The path must be unused.
func (s *WebhookServer) RegisterChat(route *chatRoute) error {
	s.chats.mu.Lock()
	defer s.chats.mu.Unlock()
	if s.chats.m == nil {
		s.chats.m = make(map[string]*chatRoute)
	}
	if _, ok := s.chats.m[route.Path]; ok {
		return fmt.Errorf("chat: path %q is already registered", route.Path)
	}
	s.chats.m[route.Path] = route
	return nil
}

// DeregisterChat stops serving the chat at path.
func (s *WebhookServer) DeregisterChat(path string) {
	s.chats.mu.Lock()
	defer s.chats.mu.Unlock()
	delete(s.chats.m, path)
}

// servesChat reports whether a chat is served at path.
func (s *WebhookServer) servesChat(path string) bool {
	s.chats.mu.RLock()
	defer s.chats.mu.RUnlock()
	return s.chats.m[path] != nil
}

// activateChat serves a trigger.chat node's chat.
func (tm *TriggerManager) activateChat(workflowID string, node *WorkflowNode) error {
	if node.Config == nil {
		if err := node.ParseConfig(); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	route, err := parseChatRoute(node.Config)
	if err != nil {
		return err
	}
	wfID, nID := workflowID, node.ID
	route.WorkflowID, route.NodeID = wfID, nID
	route.TriggerFn = func(items []Item) { tm.triggerFn(wfID, nID, items) }

	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, exists := tm.active[workflowID][node.ID]; exists {
		return nil
	}
	if err := tm.webhookServer.RegisterChat(route); err != nil {
		return err
	}
	if tm.active[workflowID] == nil {
		tm.active[workflowID] = make(map[string]*triggerEntry)
	}
	tm.active[workflowID][node.ID] = &triggerEntry{kind: "chat", webhookPath: route.Path}
	tm.logger.Info().Str("workflow_id", workflowID).Str("node_id", node.ID).Str("path", route.Path).Msg("chat trigger activated")
	return nil
}

// serveChat answers /chat/<path>: GET the page, POST <path>/message a
// message, GET <path>/messages?sessionId=&after= the session's messages.
func (s *WebhookServer) serveChat(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.Trim(rest, "/")
	path, action := rest, ""
	for _, a := range []string{"message", "messages"} {
		if p, ok := strings.CutSuffix(rest, "/"+a); ok {
			path, action = p, a
			break
		}
	}
	s.chats.mu.RLock()
	route := s.chats.m[path]
	s.chats.mu.RUnlock()
	if route == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		writeChatPage(w, route)
	case action == "message" && r.Method == http.MethodPost:
		s.chatMessage(w, r, route)
	case action == "messages" && r.Method == http.MethodGet:
		session := r.URL.Query().Get("sessionId")
		if !validChatSession(session) {
			writeJSONError(w, http.StatusBadRequest, "sessionId must be 8 to 128 letters, digits, - or _")
			return
		}
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(chatPollWait + 10*time.Second))
		msgs := ChatMessagesAfter(r.Context(), session, after, chatPollWait)
		if msgs == nil {
			msgs = []ChatMessage{}
		}
		writeWebhookJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// chatMessage takes a message ({sessionId, chatInput}, n8n's body): it
// answers the session's waiting core.respond_chat, or starts a run.
func (s *WebhookServer) chatMessage(w http.ResponseWriter, r *http.Request, route *chatRoute) {
	r.Body = http.MaxBytesReader(w, r.Body, defaultWebhookMaxBody)
	var body struct {
		SessionID string `json:"sessionId"`
		ChatInput string `json:"chatInput"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "body must be JSON {sessionId, chatInput}")
		return
	}
	if !validChatSession(body.SessionID) {
		writeJSONError(w, http.StatusBadRequest, "sessionId must be 8 to 128 letters, digits, - or _")
		return
	}
	if strings.TrimSpace(body.ChatInput) == "" {
		writeJSONError(w, http.StatusBadRequest, "chatInput must not be empty")
		return
	}
	item := map[string]interface{}{"sessionId": body.SessionID, "action": "sendMessage", "chatInput": body.ChatInput}
	PostChatMessage(body.SessionID, ChatMessage{Role: "user", Text: body.ChatInput})

	s.mu.RLock()
	run, execs := s.runner, s.execs
	h, _ := s.replies.(*replyHandler)
	s.mu.RUnlock()
	if h != nil {
		if execID, ok := h.answerChat(r.Context(), body.SessionID, item); ok {
			if execs != nil {
				go s.watchChatRun(route, execs, body.SessionID, execID, true)
			}
			writeWebhookJSON(w, http.StatusAccepted, map[string]interface{}{"execution_id": execID, "answered": true})
			return
		}
	}
	trace, status, msg := s.admitTrace(r.Context(), route.WorkflowID, "")
	if status != 0 {
		writeJSONError(w, status, msg)
		return
	}
	if trace != nil {
		item[WebhookTraceKey] = trace
	}
	items := []Item{{JSON: item}}
	if run == nil {
		route.TriggerFn(items)
		writeWebhookJSON(w, http.StatusAccepted, map[string]interface{}{"success": true})
		return
	}
	execID, err := run(route.WorkflowID, route.NodeID, items, nil)
	if err != nil {
		code, msg := webhookStartStatus(err)
		if code >= 500 && code != http.StatusServiceUnavailable {
			s.logger.Error().Err(err).Str("workflow_id", route.WorkflowID).Msg("chat: starting the run")
		}
		writeJSONError(w, code, msg)
		return
	}
	if execs != nil {
		go s.watchChatRun(route, execs, body.SessionID, execID, false)
	}
	writeWebhookJSON(w, http.StatusAccepted, map[string]interface{}{"execution_id": execID})
}

// watchChatRun follows a run a message started (or, resumed, answered)
// until it ends or pauses for a person: a run waiting only for its
// sub-workflows is resumed by the engine, so it is watched on, read less
// often the longer it waits. In lastNode mode its result is the bot's
// answer; in either mode a failed run says so in the chat. The watch ends
// with the server or the chat, and gives up on a run still going at its
// limit (chatWatchFor), saying so in the chat.
func (s *WebhookServer) watchChatRun(route *chatRoute, execs WebhookExecutions, session, execID string, resumed bool) {
	ctx, cancel := context.WithTimeout(s.watches, chatWatchFor(s.watches, execs, route.WorkflowID))
	defer cancel()
	giveUp := func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			PostChatMessage(session, ChatMessage{Role: "bot", Text: chatStillRunning})
		}
	}
	// An answered run is still WAITING until its wake resumes it.
	wakeBy := time.Now().Add(replyWakeWindow)
	pause := webhookPollInterval
	for {
		select {
		case <-ctx.Done():
			giveUp()
			return
		case <-time.After(pause):
		}
		if !s.servesChat(route.Path) {
			return // no page can read an answer any more
		}
		st, inFlight, err := execs.ExecutionInFlight(ctx, execID)
		if err != nil {
			giveUp()
			return
		}
		switch {
		case inFlight && st == "WAITING": // only for its sub-workflows
			pause = min(2*pause, chatWatchSlowest)
			continue
		case inFlight || st == "":
			pause = webhookPollInterval
			resumed = false
			continue
		case st == "WAITING":
			if resumed && time.Now().Before(wakeBy) {
				continue
			}
			return // a later answer watches the run again
		case st == "SUCCESS" || st == "SUCCESS_WITH_ERRORS":
			if route.ResponseMode != ChatRespondLastNode {
				return
			}
			// Not ctx: a run that has ended still gets its answer.
			exec, err := execs.GetExecution(s.watches, execID)
			if err != nil || exec == nil {
				return
			}
			if text := chatReplyText(lastNodeItems(exec, route.NodeID)); text != "" {
				PostChatMessage(session, ChatMessage{Role: "bot", Text: text})
			}
			return
		default:
			PostChatMessage(session, ChatMessage{Role: "bot", Text: "Sorry, something went wrong: the workflow did not finish."})
			return
		}
	}
}

// chatWatchFor is how long a chat watches a run of workflowID:
// chatWatchLimit, or the workflow's executionTimeout when that is longer.
func chatWatchFor(ctx context.Context, execs WebhookExecutions, workflowID string) time.Duration {
	limit := chatWatchLimit
	if wf, err := execs.GetWorkflow(ctx, workflowID); err == nil {
		limit = max(limit, executionTimeout(wf))
	}
	return limit
}

// chatReplyText is what the hosted chat shows for a run's result: the
// first item's "output" (an agent's answer) or "text", else the item.
func chatReplyText(items []Item) string {
	if len(items) == 0 || items[0].JSON == nil {
		return ""
	}
	j := items[0].JSON
	for _, k := range []string{"output", "text"} {
		if s, ok := j[k].(string); ok {
			return s
		}
	}
	b, _ := json.Marshal(j)
	return string(b)
}

// answerChat records item as the answer of the reply session's
// core.respond_chat waits for, if one does, and returns its execution.
func (h *replyHandler) answerChat(ctx context.Context, session string, item map[string]interface{}) (string, bool) {
	token, err := signReply("chat", session)
	if err != nil {
		return "", false
	}
	row, err := h.load(ctx, token)
	if err != nil || row == nil || row.status != WaitReplyPending || row.expired || !h.executionWaits(ctx, row.executionID) {
		return "", false
	}
	answer := make(map[string]interface{}, len(item))
	for k, v := range item {
		answer[k] = v
	}
	if ok, err := h.record(ctx, row, answer); err != nil || !ok {
		return "", false
	}
	return row.executionID, true
}

// validChatSession accepts the session IDs pages make (a UUID) and similar.
func validChatSession(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
