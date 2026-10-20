package comm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Whatever its "operation" holds, a Telegram config the node sends is one
// the send gate holds: an operation that is no string is blank to the node,
// and a blank one sends a message. The gate used to render 5 or true as
// text and let it through.
func TestTheSendGateHoldsWhatTelegramSends(t *testing.T) {
	var sends atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			sends.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"date":0,"chat":{"id":42,"type":"private"}}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	defer srv.Close()
	old := telegramAPIEndpoint
	telegramAPIEndpoint = srv.URL + "/bot%s/%s"
	t.Cleanup(func() { telegramAPIEndpoint = old })

	for _, op := range []interface{}{
		nil, true, false, float64(0), float64(5), "", " ", "5", "send_message", "send_message ", "SEND_MESSAGE",
		[]interface{}{}, []interface{}{"send_message"}, map[string]interface{}{"operation": "send_message"},
	} {
		config := map[string]interface{}{"bot_token": "abc", "chat_id": "42", "message": "Hi", "operation": op}
		before := sends.Load()
		_, _ = (&TelegramNode{}).Execute(context.Background(), workflow.NodeInput{}, config)
		if sent := sends.Load() > before; sent {
			for _, name := range []string{"comm.telegram", "telegram"} {
				if !hilsend.IsOutbound(name, config) {
					t.Errorf("%s with operation %#v sends, and the gate let it through", name, op)
				}
			}
		}
	}
	if sends.Load() == 0 {
		t.Fatal("no config sent: the fake Bot API is not reached")
	}
}
