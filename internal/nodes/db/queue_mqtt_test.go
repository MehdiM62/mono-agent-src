package dbnodes

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/monoes/mono-agent/internal/workflow"
)

// startBroker runs an in-process MQTT broker and returns its port.
func startBroker(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv := mochi.New(&mochi.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.AddListener(listeners.NewTCP(listeners.Config{ID: "t", Address: "127.0.0.1:" + strconv.Itoa(port)})); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	time.Sleep(50 * time.Millisecond)
	return port
}

func TestMQTTPublishAndSubscribe(t *testing.T) {
	port := startBroker(t)
	cred := map[string]interface{}{"host": "127.0.0.1", "port": float64(port)}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recv := make(chan workflow.Item, 4)
	done := make(chan error, 1)
	sub := map[string]interface{}{"topics": "sensors/#:1", "json_parse_body": true}
	for k, v := range cred {
		sub[k] = v
	}
	go func() { done <- SubscribeMQTT(ctx, sub, func(it workflow.Item) { recv <- it }) }()
	time.Sleep(200 * time.Millisecond) // let the subscription land

	pub := map[string]interface{}{"topic": "sensors/{{ $json.room }}", "qos": float64(1)}
	for k, v := range cred {
		pub[k] = v
	}
	out, err := (&MQTTNode{}).Execute(context.Background(), items(map[string]interface{}{"room": "kitchen", "t": 21}), pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(mainItems(t, out)) != 1 {
		t.Fatalf("publish out = %+v", out)
	}
	select {
	case it := <-recv:
		if it.JSON["topic"] != "sensors/kitchen" || it.JSON["message"].(map[string]interface{})["t"] != float64(21) {
			t.Errorf("received %v", it.JSON)
		}
	case <-ctx.Done():
		t.Fatal("no message received")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("subscribe returned %v", err)
	}
}

func TestMQTTTopicsAndConfigErrors(t *testing.T) {
	got, err := mqttTopics("a/b, c/#:2")
	if err != nil || got["a/b"] != 0 || got["c/#"] != 2 {
		t.Errorf("topics = %v %v", got, err)
	}
	if _, err := mqttTopics("x:7"); err == nil {
		t.Error("bad qos: want error")
	}
	if _, err := mqttClientOptions(map[string]interface{}{}); err == nil {
		t.Error("missing host: want error")
	}
	if _, err := mqttClientOptions(map[string]interface{}{"host": "h", "protocol": "coap"}); err == nil {
		t.Error("bad protocol: want error")
	}
}
