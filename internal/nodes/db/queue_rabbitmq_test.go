package dbnodes

import (
	"context"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/monoes/mono-agent/internal/workflow"
)

type fakeRabbit struct {
	log        []string
	published  []amqp.Publishing
	keys       []string
	deliveries chan amqp.Delivery
	acked      []uint64
}

func (f *fakeRabbit) QueueDeclare(name string, durable, autoDelete, exclusive, _ bool, args amqp.Table) (amqp.Queue, error) {
	f.log = append(f.log, "queue:"+name)
	return amqp.Queue{Name: name}, nil
}
func (f *fakeRabbit) ExchangeDeclare(name, kind string, _, _, _, _ bool, args amqp.Table) error {
	f.log = append(f.log, "exchange:"+name+":"+kind)
	if args != nil {
		f.log = append(f.log, "alt:"+args["alternate-exchange"].(string))
	}
	return nil
}
func (f *fakeRabbit) QueueBind(name, key, exchange string, _ bool, _ amqp.Table) error {
	f.log = append(f.log, "bind:"+name+":"+exchange+":"+key)
	return nil
}
func (f *fakeRabbit) PublishWithContext(_ context.Context, exchange, key string, _, _ bool, msg amqp.Publishing) error {
	f.keys = append(f.keys, exchange+"/"+key)
	f.published = append(f.published, msg)
	return nil
}
func (f *fakeRabbit) Consume(queue, _ string, autoAck, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	f.log = append(f.log, "consume:"+queue)
	return f.deliveries, nil
}
func (f *fakeRabbit) Qos(n, _ int, _ bool) error { f.log = append(f.log, "qos"); return nil }

// Acknowledger
func (f *fakeRabbit) Ack(tag uint64, _ bool) error  { f.acked = append(f.acked, tag); return nil }
func (f *fakeRabbit) Nack(uint64, bool, bool) error { return nil }
func (f *fakeRabbit) Reject(uint64, bool) error     { return nil }

func withFakeRabbit(t *testing.T) *fakeRabbit {
	f := &fakeRabbit{deliveries: make(chan amqp.Delivery, 4)}
	prev := rabbitDial
	rabbitDial = func(map[string]interface{}) (rabbitChannel, func(), error) { return f, func() {}, nil }
	t.Cleanup(func() { rabbitDial = prev })
	return f
}

func TestRabbitURL(t *testing.T) {
	u, err := rabbitURL(map[string]interface{}{"hostname": "mq", "username": "u", "password": "p@ss", "vhost": "prod", "ssl": true})
	if err != nil || u != "amqps://u:p%40ss@mq:5672/prod" {
		t.Errorf("url = %q %v", u, err)
	}
	u, _ = rabbitURL(map[string]interface{}{"hostname": "mq", "port": float64(5673)})
	if u != "amqp://mq:5673" {
		t.Errorf("default vhost url = %q", u)
	}
	if _, err := rabbitURL(map[string]interface{}{}); err == nil {
		t.Error("missing hostname: want error")
	}
}

func TestRabbitPublishQueueAndExchange(t *testing.T) {
	f := withFakeRabbit(t)
	in := items(map[string]interface{}{"id": 1})
	out, err := (&RabbitMQNode{}).Execute(context.Background(), in, map[string]interface{}{"hostname": "mq", "queue": "jobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mainItems(t, out)) != 1 || f.keys[0] != "/jobs" || string(f.published[0].Body) != `{"id":1}` || f.published[0].DeliveryMode != amqp.Persistent {
		t.Fatalf("queue publish: keys=%v body=%s", f.keys, f.published[0].Body)
	}
	_, err = (&RabbitMQNode{}).Execute(context.Background(), in, map[string]interface{}{
		"hostname": "mq", "mode": "exchange", "exchange": "ev", "exchange_type": "topic", "routing_key": "user.{{ $json.id }}",
		"send_input_data": false, "message": "hello", "alternate_exchange": "alt", "headers": map[string]interface{}{"h": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.keys[1] != "ev/user.1" || string(f.published[1].Body) != "hello" || f.published[1].Headers["h"] != "v" {
		t.Errorf("exchange publish: keys=%v msg=%+v", f.keys, f.published[1])
	}
	if strings.Join(f.log, ",") != "queue:jobs,exchange:ev:topic,alt:alt" {
		t.Errorf("log = %v", f.log)
	}
	if _, err := (&RabbitMQNode{}).Execute(context.Background(), in, map[string]interface{}{"hostname": "mq", "mode": "exchange"}); err == nil {
		t.Error("exchange mode without exchange: want error")
	}
}

func TestSubscribeRabbitMQAcksAfterEmit(t *testing.T) {
	f := withFakeRabbit(t)
	f.deliveries <- amqp.Delivery{Acknowledger: f, DeliveryTag: 7, Body: []byte(`{"a":1}`), RoutingKey: "rk"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan workflow.Item, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- SubscribeRabbitMQ(ctx, map[string]interface{}{
			"hostname": "mq", "queue": "jobs", "json_parse_body": true, "parallel_messages": float64(5),
			"bindings": []interface{}{map[string]interface{}{"exchange": "ev", "routing_key": "#"}},
		}, func(it workflow.Item) { got <- it })
	}()
	it := <-got
	if it.JSON["content"].(map[string]interface{})["a"] != float64(1) || it.JSON["fields"].(map[string]interface{})["routingKey"] != "rk" {
		t.Errorf("item = %v", it.JSON)
	}
	close(f.deliveries)
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("closed channel err = %v", err)
	}
	if len(f.acked) != 1 || f.acked[0] != 7 {
		t.Errorf("acked = %v", f.acked)
	}
	if strings.Join(f.log, ",") != "qos,queue:jobs,bind:jobs:ev:#,consume:jobs" {
		t.Errorf("log = %v", f.log)
	}
}
