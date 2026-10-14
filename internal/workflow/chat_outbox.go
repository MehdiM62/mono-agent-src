package workflow

import (
	"context"
	"sync"
	"time"
)

// The messages of hosted chat sessions (trigger.chat), by session ID: what
// the person typed and what the workflow answered. The chat page reads them
// by long polling. They live in this process, as n8n's hosted chat keeps
// them in the browser: a session is dropped once it has been idle for
// chatSessionIdle.

// ChatMessage is one message of a chat session.
type ChatMessage struct {
	ID   int    `json:"id"`
	Role string `json:"role"` // "user" or "bot"
	Text string `json:"text"`
	// Buttons are an approval's links (core.respond_chat sendAndWait).
	Buttons []ReplyLink `json:"buttons,omitempty"`
	// BlockInput asks the page to take no typed input until the person
	// answers with a button.
	BlockInput bool      `json:"blockInput,omitempty"`
	At         time.Time `json:"at"`
}

// chatSessionIdle is how long a session's messages are kept after its last
// message.
const chatSessionIdle = 24 * time.Hour

type chatSession struct {
	msgs    []ChatMessage
	touched time.Time
	// changed is closed (and replaced) when a message is added.
	changed chan struct{}
}

var chatSessions = struct {
	sync.Mutex
	m map[string]*chatSession
}{m: map[string]*chatSession{}}

// PostChatMessage adds msg to sessionID's chat and wakes the pages waiting
// for it.
func PostChatMessage(sessionID string, msg ChatMessage) {
	chatSessions.Lock()
	defer chatSessions.Unlock()
	now := time.Now()
	s := chatSessionLocked(sessionID)
	msg.ID, msg.At = len(s.msgs)+1, now.UTC()
	s.msgs = append(s.msgs, msg)
	s.touched = now
	close(s.changed)
	s.changed = make(chan struct{})
}

func chatSessionLocked(sessionID string) *chatSession {
	s := chatSessions.m[sessionID]
	if s == nil {
		for id, old := range chatSessions.m {
			if time.Since(old.touched) > chatSessionIdle {
				delete(chatSessions.m, id)
			}
		}
		s = &chatSession{touched: time.Now(), changed: make(chan struct{})}
		chatSessions.m[sessionID] = s
	}
	return s
}

// ChatMessagesAfter returns sessionID's messages after the one numbered
// after, waiting up to wait for one when there are none yet.
func ChatMessagesAfter(ctx context.Context, sessionID string, after int, wait time.Duration) []ChatMessage {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		chatSessions.Lock()
		s := chatSessionLocked(sessionID)
		var out []ChatMessage
		if after < len(s.msgs) {
			out = append(out, s.msgs[max(after, 0):]...)
		}
		changed := s.changed
		chatSessions.Unlock()
		if len(out) > 0 {
			return out
		}
		select {
		case <-changed:
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}
