package dbnodes

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/monoes/mono-agent/internal/workflow"
)

// rabbitChannel is the part of *amqp.Channel the RabbitMQ node and consumer
// use; tests substitute a fake.
type rabbitChannel interface {
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Qos(prefetchCount, prefetchSize int, global bool) error
}

// rabbitURL builds amqp(s)://user:pass@host:port/vhost from the credential
// keys, or returns 'url' when one is set.
func rabbitURL(cfg map[string]interface{}) (string, error) {
	if u := cfgString(cfg, "url"); u != "" {
		return u, nil
	}
	host := cfgString(cfg, "hostname")
	if host == "" {
		return "", fmt.Errorf("'hostname' (or 'url') is required")
	}
	scheme := "amqp"
	if cfgBool(cfg, "ssl", false) {
		scheme = "amqps"
	}
	port, ok := cfgInt(cfg, "port")
	if !ok || port == 0 {
		port = 5672
	}
	u := url.URL{Scheme: scheme, Host: host + ":" + strconv.Itoa(port)}
	if user := cfgString(cfg, "username"); user != "" {
		u.User = url.UserPassword(user, cfgString(cfg, "password"))
	}
	// An empty path selects the default vhost "/".
	if vhost := cfgString(cfg, "vhost"); vhost != "" && vhost != "/" {
		u.Path = "/" + vhost
		u.RawPath = "/" + url.PathEscape(vhost)
	}
	return u.String(), nil
}

// rabbitDial opens a channel; tests swap it.
var rabbitDial = func(cfg map[string]interface{}) (rabbitChannel, func(), error) {
	addr, err := rabbitURL(cfg)
	if err != nil {
		return nil, nil, err
	}
	tc, err := tlsFromConfig(cfg, false)
	if err != nil {
		return nil, nil, err
	}
	var conn *amqp.Connection
	if tc != nil {
		conn, err = amqp.DialTLS(addr, tc)
	} else {
		conn, err = amqp.Dial(addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("open channel: %w", err)
	}
	return ch, func() { ch.Close(); conn.Close() }, nil
}

func amqpTable(v interface{}) (amqp.Table, error) {
	m, err := toKVMap(v)
	if err != nil || len(m) == 0 {
		return nil, err
	}
	return amqp.Table(m), nil
}

// RabbitMQNode publishes one message per input item to a queue or exchange.
// Type: "queue.rabbitmq"
type RabbitMQNode struct{}

func (n *RabbitMQNode) Type() string { return "queue.rabbitmq" }

// rabbitDestinations are the broker the connection's password and client
// certificate go to: hostname and port, or a url in their place.
var rabbitDestinations = [][]string{{"hostname", "url"}, {"port"}}

// CredentialDestinationKeys: the broker the password goes to
// (workflow.CheckCredentialDestinations).
func (n *RabbitMQNode) CredentialDestinationKeys() [][]string { return rabbitDestinations }
func (n *RabbitMQNode) PerItemConfigFields() []string         { return queuePerItemFields }

func (n *RabbitMQNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if op := cfgString(config, "operation"); op != "" && op != "sendMessage" {
		return nil, fmt.Errorf("queue.rabbitmq: unsupported operation %q (only sendMessage; deleteMessage is a trigger-side acknowledgement)", op)
	}
	ch, closeFn, err := rabbitDial(config)
	if err != nil {
		return nil, fmt.Errorf("queue.rabbitmq: %w", err)
	}
	defer closeFn()
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, queuePerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("queue.rabbitmq: %w", err)
		}
		if err := rabbitPublish(ctx, ch, cfg, item); err != nil {
			return nil, fmt.Errorf("queue.rabbitmq (item %d): %w", i, err)
		}
		out = append(out, item)
	}
	return mainOutputs(out, nil), nil
}

func rabbitPublish(ctx context.Context, ch rabbitChannel, cfg map[string]interface{}, item workflow.Item) error {
	body, err := messagePayload(item, cfgBool(cfg, "send_input_data", true), cfg["message"])
	if err != nil {
		return err
	}
	args, err := amqpTable(cfg["arguments"])
	if err != nil {
		return fmt.Errorf("'arguments': %w", err)
	}
	headers, err := amqpTable(cfg["headers"])
	if err != nil {
		return fmt.Errorf("'headers': %w", err)
	}
	durable, autoDelete, exclusive := cfgBool(cfg, "durable", true), cfgBool(cfg, "auto_delete", false), cfgBool(cfg, "exclusive", false)
	msg := amqp.Publishing{Body: body, Headers: headers, ContentType: "text/plain"}
	if cfgBool(cfg, "send_input_data", true) {
		msg.ContentType = "application/json"
	}
	if durable {
		msg.DeliveryMode = amqp.Persistent
	}

	exchange, routingKey := "", ""
	switch mode := cfgString(cfg, "mode"); mode {
	case "", "queue":
		queue := cfgString(cfg, "queue")
		if queue == "" {
			return fmt.Errorf("'queue' is required in queue mode")
		}
		if cfgBool(cfg, "assert_queue", true) {
			if _, err := ch.QueueDeclare(queue, durable, autoDelete, exclusive, false, args); err != nil {
				return fmt.Errorf("declare queue: %w", err)
			}
		}
		routingKey = queue
	case "exchange":
		exchange = cfgString(cfg, "exchange")
		if exchange == "" {
			return fmt.Errorf("'exchange' is required in exchange mode")
		}
		kind := cfgString(cfg, "exchange_type")
		if kind == "" {
			kind = "fanout"
		}
		if alt := cfgString(cfg, "alternate_exchange"); alt != "" {
			if args == nil {
				args = amqp.Table{}
			}
			args["alternate-exchange"] = alt
		}
		if cfgBool(cfg, "assert_exchange", true) {
			if err := ch.ExchangeDeclare(exchange, kind, durable, autoDelete, false, false, args); err != nil {
				return fmt.Errorf("declare exchange: %w", err)
			}
		}
		routingKey = cfgString(cfg, "routing_key")
	default:
		return fmt.Errorf("'mode' must be queue or exchange (got %q)", mode)
	}
	return ch.PublishWithContext(ctx, exchange, routingKey, false, false, msg)
}

// SubscribeRabbitMQ consumes 'queue' (declared unless assert_queue is false,
// and bound to any 'bindings' [{exchange, routing_key}]) and calls emit per
// message, acknowledging after emit returns (acknowledge "immediately"
// acks on receipt instead). parallel_messages > 0 sets the prefetch count.
// Keys: rabbit credential keys, queue, bindings, json_parse_body,
// only_content, content_is_binary, acknowledge, parallel_messages.
func SubscribeRabbitMQ(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	queue := cfgString(cfg, "queue")
	if queue == "" {
		return fmt.Errorf("trigger.rabbitmq: 'queue' is required")
	}
	ch, closeFn, err := rabbitDial(cfg)
	if err != nil {
		return fmt.Errorf("trigger.rabbitmq: %w", err)
	}
	defer closeFn()
	if err := rabbitPrepareConsumer(ch, queue, cfg); err != nil {
		return fmt.Errorf("trigger.rabbitmq: %w", err)
	}
	autoAck := cfgString(cfg, "acknowledge") == "immediately"
	deliveries, err := ch.Consume(queue, "", autoAck, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("trigger.rabbitmq: consume: %w", err)
	}
	return rabbitLoop(ctx, deliveries, autoAck, cfg, emit)
}

func rabbitPrepareConsumer(ch rabbitChannel, queue string, cfg map[string]interface{}) error {
	if n, ok := cfgInt(cfg, "parallel_messages"); ok && n > 0 {
		if err := ch.Qos(n, 0, false); err != nil {
			return fmt.Errorf("qos: %w", err)
		}
	}
	args, err := amqpTable(cfg["arguments"])
	if err != nil {
		return fmt.Errorf("'arguments': %w", err)
	}
	if cfgBool(cfg, "assert_queue", true) {
		if _, err := ch.QueueDeclare(queue, cfgBool(cfg, "durable", true), cfgBool(cfg, "auto_delete", false), cfgBool(cfg, "exclusive", false), false, args); err != nil {
			return fmt.Errorf("declare queue: %w", err)
		}
	}
	bindings, err := toList(cfg["bindings"])
	if err != nil {
		return fmt.Errorf("'bindings': %w", err)
	}
	for i, b := range bindings {
		m, _ := b.(map[string]interface{})
		ex := cfgString(m, "exchange")
		if ex == "" {
			return fmt.Errorf("'bindings'[%d]: 'exchange' is required", i)
		}
		if err := ch.QueueBind(queue, cfgString(m, "routing_key"), ex, false, nil); err != nil {
			return fmt.Errorf("bind to %s: %w", ex, err)
		}
	}
	return nil
}

func rabbitLoop(ctx context.Context, deliveries <-chan amqp.Delivery, autoAck bool, cfg map[string]interface{}, emit func(workflow.Item)) error {
	jsonParse, onlyContent, binary := cfgBool(cfg, "json_parse_body", false), cfgBool(cfg, "only_content", false), cfgBool(cfg, "content_is_binary", false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("trigger.rabbitmq: delivery channel closed by the broker")
			}
			emit(rabbitItem(d, jsonParse, onlyContent, binary))
			if !autoAck {
				if err := d.Ack(false); err != nil {
					return fmt.Errorf("trigger.rabbitmq: ack: %w", err)
				}
			}
		}
	}
}

func rabbitItem(d amqp.Delivery, jsonParse, onlyContent, binary bool) workflow.Item {
	if binary {
		return workflow.Item{
			JSON:   map[string]interface{}{"routingKey": d.RoutingKey, "exchange": d.Exchange},
			Binary: map[string][]byte{"data": d.Body},
		}
	}
	content := parseMessageBody(d.Body, jsonParse)
	if onlyContent {
		if m, ok := content.(map[string]interface{}); ok {
			return workflow.NewItem(m)
		}
		return workflow.NewItem(map[string]interface{}{"content": content})
	}
	headers := map[string]interface{}{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	return workflow.NewItem(map[string]interface{}{
		"content":    content,
		"fields":     map[string]interface{}{"routingKey": d.RoutingKey, "exchange": d.Exchange, "deliveryTag": d.DeliveryTag, "redelivered": d.Redelivered},
		"properties": map[string]interface{}{"contentType": d.ContentType, "headers": headers, "messageId": d.MessageId, "correlationId": d.CorrelationId},
	})
}
