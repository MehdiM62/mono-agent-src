package dbnodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	amqp1 "github.com/Azure/go-amqp"

	"github.com/monoes/mono-agent/internal/workflow"
)

// AMQP 1.0 (ActiveMQ Artemis, Azure Service Bus, Qpid, Solace, …) via the
// pure-Go github.com/Azure/go-amqp client.

type amqp1Sender interface {
	Send(ctx context.Context, msg *amqp1.Message, opts *amqp1.SendOptions) error
}

type amqp1Receiver interface {
	Receive(ctx context.Context, opts *amqp1.ReceiveOptions) (*amqp1.Message, error)
	AcceptMessage(ctx context.Context, msg *amqp1.Message) error
}

// amqp1Session dials and opens a session from the credential keys
// (hostname, port, username, password, transport_type tcp|tls, container_id).
func amqp1Session(ctx context.Context, cfg map[string]interface{}) (*amqp1.Session, func(), error) {
	host := cfgString(cfg, "hostname")
	if host == "" {
		return nil, nil, fmt.Errorf("'hostname' is required")
	}
	port, ok := cfgInt(cfg, "port")
	if !ok || port == 0 {
		port = 5672
	}
	scheme := "amqp"
	if cfgString(cfg, "transport_type") == "tls" {
		scheme = "amqps"
	}
	opts := &amqp1.ConnOptions{ContainerID: cfgString(cfg, "container_id")}
	if id := cfgString(cfg, "client_name"); id != "" && opts.ContainerID == "" {
		opts.ContainerID = id
	}
	if user := cfgString(cfg, "username"); user != "" {
		opts.SASLType = amqp1.SASLTypePlain(user, cfgString(cfg, "password"))
	} else {
		opts.SASLType = amqp1.SASLTypeAnonymous()
	}
	conn, err := amqp1.Dial(ctx, scheme+"://"+host+":"+strconv.Itoa(port), opts)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	sess, err := conn.NewSession(ctx, nil)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("open session: %w", err)
	}
	return sess, func() { conn.Close() }, nil
}

// amqp1OpenSender / amqp1OpenReceiver are swapped by tests.
var amqp1OpenSender = func(ctx context.Context, cfg map[string]interface{}, sink string) (amqp1Sender, func(), error) {
	sess, closeFn, err := amqp1Session(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	s, err := sess.NewSender(ctx, sink, nil)
	if err != nil {
		closeFn()
		return nil, nil, fmt.Errorf("open sender: %w", err)
	}
	return s, closeFn, nil
}

var amqp1OpenReceiver = func(ctx context.Context, cfg map[string]interface{}, sink string, opts *amqp1.ReceiverOptions) (amqp1Receiver, func(), error) {
	sess, closeFn, err := amqp1Session(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	r, err := sess.NewReceiver(ctx, sink, opts)
	if err != nil {
		closeFn()
		return nil, nil, fmt.Errorf("open receiver: %w", err)
	}
	return r, closeFn, nil
}

// AMQPNode sends one AMQP 1.0 message per input item to 'sink'.
// Type: "queue.amqp"
type AMQPNode struct{}

func (n *AMQPNode) Type() string                  { return "queue.amqp" }
func (n *AMQPNode) PerItemConfigFields() []string { return queuePerItemFields }

func (n *AMQPNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	sink := cfgString(config, "sink")
	if sink == "" {
		return nil, fmt.Errorf("queue.amqp: 'sink' (queue or topic address) is required")
	}
	sender, closeFn, err := amqp1OpenSender(ctx, config, sink)
	if err != nil {
		return nil, fmt.Errorf("queue.amqp: %w", err)
	}
	defer closeFn()
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, queuePerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("queue.amqp: %w", err)
		}
		msg, err := amqp1Message(cfg, item)
		if err != nil {
			return nil, fmt.Errorf("queue.amqp (item %d): %w", i, err)
		}
		if err := sender.Send(ctx, msg, nil); err != nil {
			return nil, fmt.Errorf("queue.amqp (item %d): send failed: %w", i, err)
		}
		out = append(out, item)
	}
	return mainOutputs(out, nil), nil
}

func amqp1Message(cfg map[string]interface{}, item workflow.Item) (*amqp1.Message, error) {
	var payload interface{} = item.JSON
	if prop := cfgString(cfg, "send_only_property"); prop != "" {
		payload = item.JSON[prop]
	}
	var msg *amqp1.Message
	if cfgBool(cfg, "data_as_object", false) {
		msg = &amqp1.Message{Value: payload}
	} else {
		var body []byte
		if s, ok := payload.(string); ok {
			body = []byte(s)
		} else {
			b, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("encode message: %w", err)
			}
			body = b
		}
		msg = amqp1.NewMessage(body)
	}
	props, err := toMap(cfg["header_parameters"])
	if err != nil {
		return nil, fmt.Errorf("'header_parameters': %w", err)
	}
	if len(props) > 0 {
		msg.ApplicationProperties = props
	}
	return msg, nil
}

// SubscribeAMQP receives from 'sink' and calls emit per message, accepting
// it after emit returns. With 'subscription' set the link is a durable
// subscription named after it (pair with 'client_name' as container id).
// Keys: amqp credential keys, sink, client_name, subscription,
// pull_messages_number (link credit, default 100), json_parse_body,
// only_body, json_convert_byte_array_to_string.
func SubscribeAMQP(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	sink := cfgString(cfg, "sink")
	if sink == "" {
		return fmt.Errorf("trigger.amqp: 'sink' is required")
	}
	opts := &amqp1.ReceiverOptions{Credit: 100}
	if n, ok := cfgInt(cfg, "pull_messages_number"); ok && n > 0 {
		opts.Credit = int32(n)
	}
	if sub := cfgString(cfg, "subscription"); sub != "" {
		opts.Name = sub
		opts.Durability = amqp1.DurabilityUnsettledState
		opts.ExpiryPolicy = amqp1.ExpiryPolicyNever
	}
	r, closeFn, err := amqp1OpenReceiver(ctx, cfg, sink, opts)
	if err != nil {
		return fmt.Errorf("trigger.amqp: %w", err)
	}
	defer closeFn()
	jsonParse, onlyBody := cfgBool(cfg, "json_parse_body", false), cfgBool(cfg, "only_body", false)
	for {
		msg, err := r.Receive(ctx, nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("trigger.amqp: receive: %w", err)
		}
		emit(amqp1Item(msg, jsonParse, onlyBody))
		if err := r.AcceptMessage(ctx, msg); err != nil && ctx.Err() == nil {
			return fmt.Errorf("trigger.amqp: accept: %w", err)
		}
	}
}

func amqp1Item(msg *amqp1.Message, jsonParse, onlyBody bool) workflow.Item {
	var body interface{}
	switch {
	case msg.Value != nil:
		body = msg.Value
		if b, ok := body.([]byte); ok {
			body = parseMessageBody(b, jsonParse)
		} else if s, ok := body.(string); ok && jsonParse {
			body = parseMessageBody([]byte(s), true)
		}
	default:
		body = parseMessageBody(msg.GetData(), jsonParse)
	}
	if onlyBody {
		if m, ok := body.(map[string]interface{}); ok {
			return workflow.NewItem(m)
		}
		return workflow.NewItem(map[string]interface{}{"body": body})
	}
	data := map[string]interface{}{"body": body}
	if len(msg.ApplicationProperties) > 0 {
		data["application_properties"] = msg.ApplicationProperties
	}
	if p := msg.Properties; p != nil {
		props := map[string]interface{}{}
		if p.MessageID != nil {
			props["message_id"] = fmt.Sprint(p.MessageID)
		}
		if p.Subject != nil {
			props["subject"] = *p.Subject
		}
		if p.To != nil {
			props["to"] = *p.To
		}
		if len(props) > 0 {
			data["properties"] = props
		}
	}
	return workflow.NewItem(data)
}
