package workflow

import (
	"context"
	"testing"
	"time"
)

func TestParseChatRoute(t *testing.T) {
	r, err := parseChatRoute(map[string]interface{}{"path": "/help/", "initialMessages": "Hi!\n\nAsk me."})
	if err != nil || r.Path != "help" || r.ResponseMode != ChatRespondLastNode || len(r.InitialMessages) != 2 {
		t.Fatalf("route = %+v, %v", r, err)
	}
	for _, cfg := range []map[string]interface{}{{}, {"path": "x", "responseMode": "streaming"}} {
		if _, err := parseChatRoute(cfg); err == nil {
			t.Errorf("%v was accepted", cfg)
		}
	}
}

func TestChatMessagesAfterWaits(t *testing.T) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		PostChatMessage("wait-session", ChatMessage{Role: "bot", Text: "late"})
	}()
	got := ChatMessagesAfter(context.Background(), "wait-session", 0, 5*time.Second)
	if len(got) != 1 || got[0].Text != "late" || got[0].ID != 1 {
		t.Fatalf("messages = %+v", got)
	}
	if got := ChatMessagesAfter(context.Background(), "wait-session", 1, 10*time.Millisecond); got != nil {
		t.Fatalf("nothing new, got %+v", got)
	}
}

func TestChatReplyText(t *testing.T) {
	for want, items := range map[string][]Item{
		"agent says": {{JSON: map[string]interface{}{"output": "agent says", "text": "x"}}},
		"t":          {{JSON: map[string]interface{}{"text": "t"}}},
		`{"n":1}`:    {{JSON: map[string]interface{}{"n": 1}}},
		"":           nil,
	} {
		if got := chatReplyText(items); got != want {
			t.Errorf("chatReplyText(%v) = %q, want %q", items, got, want)
		}
	}
}
