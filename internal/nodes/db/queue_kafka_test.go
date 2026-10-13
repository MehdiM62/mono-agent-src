package dbnodes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestKafkaProduceThenSubscribe(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	brokers := strings.Join(cluster.ListenAddrs(), ",")

	out, err := (&KafkaNode{}).Execute(context.Background(),
		items(map[string]interface{}{"id": 1}, map[string]interface{}{"id": 2}),
		map[string]interface{}{
			"brokers": brokers, "topic": "events", "message_key": "k-{{ $json.id }}",
			"headers": map[string]interface{}{"source": "monoagent"}, "acks": true, "compression": "gzip",
		})
	if err != nil {
		t.Fatal(err)
	}
	got := mainItems(t, out)
	if len(got) != 2 || got[0].JSON["success"] != true || got[1].JSON["offset"] != int64(1) {
		t.Fatalf("produce out = %+v", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	recv := make(chan workflow.Item, 4)
	done := make(chan error, 1)
	go func() {
		done <- SubscribeKafka(ctx, map[string]interface{}{
			"brokers": brokers, "topic": "events", "group_id": "g1", "json_parse_message": true, "return_headers": true,
		}, func(it workflow.Item) { recv <- it })
	}()
	var msgs []workflow.Item
	for len(msgs) < 2 {
		select {
		case it := <-recv:
			msgs = append(msgs, it)
		case <-ctx.Done():
			t.Fatalf("timed out; got %d messages", len(msgs))
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("subscribe returned %v", err)
	}
	m := msgs[0].JSON
	if m["key"] != "k-1" || m["message"].(map[string]interface{})["id"] != float64(1) || m["headers"].(map[string]interface{})["source"] != "monoagent" {
		t.Errorf("first message = %v", m)
	}
}

func TestKafkaConfigErrors(t *testing.T) {
	if _, err := (&KafkaNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{"topic": "t"}); err == nil || !strings.Contains(err.Error(), "brokers") {
		t.Errorf("missing brokers err = %v", err)
	}
	if _, err := (&KafkaNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{"brokers": "x:1", "compression": "brotli"}); err == nil {
		t.Error("bad compression: want error")
	}
	if err := SubscribeKafka(context.Background(), map[string]interface{}{"brokers": "x:1"}, nil); err == nil {
		t.Error("missing topic/group: want error")
	}
	if _, err := kafkaClientOpts(map[string]interface{}{"brokers": "x:1", "authentication": true, "sasl_mechanism": "gssapi"}); err == nil {
		t.Error("bad sasl mechanism: want error")
	}
}
