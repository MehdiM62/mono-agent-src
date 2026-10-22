package chat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/monomind"
)

const resultFieldsTranscript = `  echo '{"v":1,"type":"start","runtime":"claude","cwd":"/w","pid":1,"access":"full"}'
  echo '{"v":1,"type":"assistant","text":"done"}'
  echo '{"v":1,"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","text":"done","effort":"high","peak_context_tokens":250000,"context_warning":true,"unexpected_models":["claude-opus-4-1"],"model_usage":{"claude-opus-4-1":{"input":1,"output":2,"cache_read":3,"cache_creation":4}},"agent_launches":{"total":2,"review":1}}'
  echo '{"v":1,"type":"done","exit_code":0}'`

func TestCoderTurnJournalsUnexpectedModelsAndContextWarning(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	bin, _ := writeCoderMonomind(t, resultFieldsTranscript)
	withCoderCaps(t, monomind.CoderCapabilities...)
	setCoderSettings(t, dbPath, coderSettings{Enabled: true})
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	out, err := runChatCmd(t, dbPath, bin, "--conversation", conv.ID, "--turn", "turn-1", "--", "go")
	if err != nil {
		t.Fatalf("turn: %v\n%s", err, out)
	}
	var got []string
	for _, n := range parseJournaledTurn(t, out).byType(chatevents.EventNotice) {
		var p chatevents.NoticePayload
		json.Unmarshal(n.Payload, &p)
		if p.Code == noticeCoderResult {
			got = append(got, p.Message)
			if p.Severity != chatevents.SeverityWarning {
				t.Errorf("severity = %v", p.Severity)
			}
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], "claude-opus-4-1") || !strings.Contains(got[1], "250000") {
		t.Errorf("coder.result notices = %q", got)
	}
}

func TestResultFieldsRoundTrip(t *testing.T) {
	var ev monomind.Event
	line := `{"v":1,"type":"result","effort":"high","peak_context_tokens":5,"agent_launches":{"total":2,"review":1},"model_usage":{"m":{"input":1,"output":2,"cache_read":3,"cache_creation":4}}}`
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Effort != "high" || ev.PeakContextTokens != 5 || ev.AgentLaunches == nil || ev.AgentLaunches.Review != 1 || ev.ModelUsage["m"].CacheCreation != 4 {
		t.Errorf("decoded %+v", ev.ResultFields)
	}
	b, _ := json.Marshal(ev)
	if !strings.Contains(string(b), `"agent_launches":{"total":2,"review":1}`) || strings.Contains(string(b), "context_warning") {
		t.Errorf("re-encoded %s", b)
	}
}

func TestCoderTurnCompactsAWarnedSessionInsteadOfWarning(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	transcript := `  echo '{"v":1,"type":"session","session_id":"sess-1"}'
` + resultFieldsTranscript
	bin, argsLog := writeCoderMonomind(t, transcript)
	withCoderCaps(t, monomind.CoderCapabilities...)
	setCoderSettings(t, dbPath, coderSettings{Enabled: true})
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	out, err := runChatCmd(t, dbPath, bin, "--conversation", conv.ID, "--turn", "turn-1", "--", "go")
	if err != nil {
		t.Fatalf("turn: %v\n%s", err, out)
	}
	args, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(args), "--resume sess-1") {
		t.Errorf("the session was not resumed to compact it: %q", args)
	}
	compacted := false
	for _, n := range parseJournaledTurn(t, out).byType(chatevents.EventNotice) {
		var p chatevents.NoticePayload
		json.Unmarshal(n.Payload, &p)
		if p.Code == noticeCoderResult && strings.Contains(p.Message, "every further call") {
			t.Errorf("the warning still shows: %q", p.Message)
		}
		compacted = compacted || strings.Contains(p.Message, "was compacted")
	}
	if !compacted {
		t.Errorf("no compaction notice:\n%s", out)
	}
}
