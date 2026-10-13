package dbnodes

import (
	"context"
	"errors"
	"testing"

	amqp1 "github.com/Azure/go-amqp"

	"github.com/monoes/mono-agent/internal/workflow"
)

type fakeAMQPSender struct{ sent []*amqp1.Message }

func (f *fakeAMQPSender) Send(_ context.Context, m *amqp1.Message, _ *amqp1.SendOptions) error {
	f.sent = append(f.sent, m)
	return nil
}

type fakeAMQPReceiver struct {
	msgs     []*amqp1.Message
	accepted int
	cancel   context.CancelFunc
}

func (f *fakeAMQPReceiver) Receive(ctx context.Context, _ *amqp1.ReceiveOptions) (*amqp1.Message, error) {
	if len(f.msgs) == 0 {
		f.cancel()
		return nil, errors.New("link detached")
	}
	m := f.msgs[0]
	f.msgs = f.msgs[1:]
	return m, nil
}
func (f *fakeAMQPReceiver) AcceptMessage(context.Context, *amqp1.Message) error {
	f.accepted++
	return nil
}

func TestAMQPSendBodiesAndProperties(t *testing.T) {
	f := &fakeAMQPSender{}
	var gotSink string
	prev := amqp1OpenSender
	amqp1OpenSender = func(_ context.Context, _ map[string]interface{}, sink string) (amqp1Sender, func(), error) {
		gotSink = sink
		return f, func() {}, nil
	}
	defer func() { amqp1OpenSender = prev }()

	in := items(map[string]interface{}{"id": 1, "text": "hi"})
	if _, err := (&AMQPNode{}).Execute(context.Background(), in, map[string]interface{}{
		"hostname": "h", "sink": "queue://jobs", "header_parameters": `{"tenant": "acme"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&AMQPNode{}).Execute(context.Background(), in, map[string]interface{}{
		"hostname": "h", "sink": "topic://t", "send_only_property": "text", "data_as_object": true,
	}); err != nil {
		t.Fatal(err)
	}
	if gotSink != "topic://t" || string(f.sent[0].GetData()) != `{"id":1,"text":"hi"}` || f.sent[0].ApplicationProperties["tenant"] != "acme" {
		t.Errorf("first message = %+v", f.sent[0])
	}
	if f.sent[1].Value != "hi" {
		t.Errorf("value message = %+v", f.sent[1])
	}
	if _, err := (&AMQPNode{}).Execute(context.Background(), in, map[string]interface{}{"hostname": "h"}); err == nil {
		t.Error("missing sink: want error")
	}
}

func TestSubscribeAMQPDurableAndAccept(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subject := "s"
	r := &fakeAMQPReceiver{cancel: cancel, msgs: []*amqp1.Message{
		{Data: [][]byte{[]byte(`{"a":1}`)}, ApplicationProperties: map[string]any{"k": "v"}, Properties: &amqp1.MessageProperties{Subject: &subject}},
		{Value: "plain"},
	}}
	var gotOpts *amqp1.ReceiverOptions
	prev := amqp1OpenReceiver
	amqp1OpenReceiver = func(_ context.Context, _ map[string]interface{}, _ string, o *amqp1.ReceiverOptions) (amqp1Receiver, func(), error) {
		gotOpts = o
		return r, func() {}, nil
	}
	defer func() { amqp1OpenReceiver = prev }()

	var got []workflow.Item
	err := SubscribeAMQP(ctx, map[string]interface{}{"hostname": "h", "sink": "topic://t", "subscription": "sub1", "json_parse_body": true},
		func(it workflow.Item) { got = append(got, it) })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotOpts.Name != "sub1" || gotOpts.Durability != amqp1.DurabilityUnsettledState || gotOpts.Credit != 100 {
		t.Errorf("receiver opts = %+v", gotOpts)
	}
	if len(got) != 2 || r.accepted != 2 {
		t.Fatalf("got %d items, accepted %d", len(got), r.accepted)
	}
	if got[0].JSON["body"].(map[string]interface{})["a"] != float64(1) || got[0].JSON["properties"].(map[string]interface{})["subject"] != "s" {
		t.Errorf("item0 = %v", got[0].JSON)
	}
	if got[1].JSON["body"] != "plain" {
		t.Errorf("item1 = %v", got[1].JSON)
	}
}
