package dbnodes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/monoes/mono-agent/internal/workflow"
)

const mqttTimeout = 30 * time.Second

// mqttClientOptions builds client options from the credential keys
// (protocol mqtt|mqtts|ws|wss, host, port, username, password, client_id,
// clean, ssl, ca/cert/key, reject_unauthorized).
func mqttClientOptions(cfg map[string]interface{}) (*mqtt.ClientOptions, error) {
	host := cfgString(cfg, "host")
	if host == "" {
		return nil, fmt.Errorf("'host' is required")
	}
	proto := strings.ToLower(cfgString(cfg, "protocol"))
	switch proto {
	case "", "mqtt":
		proto = "tcp"
	case "mqtts":
		proto = "ssl"
	case "ws", "wss":
	default:
		return nil, fmt.Errorf("'protocol' must be mqtt, mqtts, ws or wss")
	}
	port, ok := cfgInt(cfg, "port")
	if !ok || port == 0 {
		port = 1883
	}
	o := mqtt.NewClientOptions().AddBroker(proto + "://" + host + ":" + strconv.Itoa(port))
	id := cfgString(cfg, "client_id")
	if id == "" {
		id = "monoagent_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	o.SetClientID(id).SetCleanSession(cfgBool(cfg, "clean", true)).SetConnectTimeout(mqttTimeout)
	if u := cfgString(cfg, "username"); u != "" {
		o.SetUsername(u).SetPassword(cfgString(cfg, "password"))
	}
	tc, err := tlsFromConfig(cfg, proto == "ssl" || proto == "wss")
	if err != nil {
		return nil, err
	}
	if tc != nil {
		o.SetTLSConfig(tc)
	}
	return o, nil
}

func mqttConnect(o *mqtt.ClientOptions) (mqtt.Client, error) {
	c := mqtt.NewClient(o)
	tok := c.Connect()
	if !tok.WaitTimeout(mqttTimeout) {
		return nil, fmt.Errorf("connect timed out")
	}
	if err := tok.Error(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return c, nil
}

// MQTTNode publishes one message per input item.
// Type: "queue.mqtt"
type MQTTNode struct{}

func (n *MQTTNode) Type() string { return "queue.mqtt" }

// mqttDestinations are the broker the connection's password goes to.
var mqttDestinations = [][]string{{"host"}, {"port"}}

// CredentialDestinationKeys: the broker the password goes to
// (workflow.CheckCredentialDestinations).
func (n *MQTTNode) CredentialDestinationKeys() [][]string { return mqttDestinations }
func (n *MQTTNode) PerItemConfigFields() []string         { return queuePerItemFields }

func (n *MQTTNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	o, err := mqttClientOptions(config)
	if err != nil {
		return nil, fmt.Errorf("queue.mqtt: %w", err)
	}
	qos, _ := cfgInt(config, "qos")
	if qos < 0 || qos > 2 {
		return nil, fmt.Errorf("queue.mqtt: 'qos' must be 0, 1 or 2")
	}
	c, err := mqttConnect(o)
	if err != nil {
		return nil, fmt.Errorf("queue.mqtt: %w", err)
	}
	defer c.Disconnect(250)
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, queuePerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("queue.mqtt: %w", err)
		}
		topic := cfgString(cfg, "topic")
		if topic == "" {
			return nil, fmt.Errorf("queue.mqtt: 'topic' is required")
		}
		payload, err := messagePayload(item, cfgBool(cfg, "send_input_data", true), cfg["message"])
		if err != nil {
			return nil, fmt.Errorf("queue.mqtt (item %d): %w", i, err)
		}
		tok := c.Publish(topic, byte(qos), cfgBool(cfg, "retain", false), payload)
		select {
		case <-tok.Done():
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := tok.Error(); err != nil {
			return nil, fmt.Errorf("queue.mqtt (item %d): publish failed: %w", i, err)
		}
		out = append(out, item)
	}
	return mainOutputs(out, nil), nil
}

// mqttTopics parses "a/b, c/#:1" into topic -> QoS (default 0).
func mqttTopics(v interface{}) (map[string]byte, error) {
	out := map[string]byte{}
	for _, t := range toStringList(v) {
		topic, qos := t, 0
		if i := strings.LastIndexByte(t, ':'); i > 0 {
			if q, err := strconv.Atoi(t[i+1:]); err == nil {
				if q < 0 || q > 2 {
					return nil, fmt.Errorf("topic %q: QoS must be 0, 1 or 2", t)
				}
				topic, qos = t[:i], q
			}
		}
		out[topic] = byte(qos)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("'topics' is required")
	}
	return out, nil
}

// SubscribeMQTT subscribes to 'topics' (comma-separated, optional ":qos"
// suffix) and calls emit per message until ctx is cancelled or the
// connection drops. Keys: mqtt credential keys, topics, json_parse_body,
// only_message.
func SubscribeMQTT(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	topics, err := mqttTopics(cfg["topics"])
	if err != nil {
		return fmt.Errorf("trigger.mqtt: %w", err)
	}
	o, err := mqttClientOptions(cfg)
	if err != nil {
		return fmt.Errorf("trigger.mqtt: %w", err)
	}
	lost := make(chan error, 1)
	o.SetAutoReconnect(false).SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		select {
		case lost <- err:
		default:
		}
	})
	c, err := mqttConnect(o)
	if err != nil {
		return fmt.Errorf("trigger.mqtt: %w", err)
	}
	defer c.Disconnect(250)
	jsonParse, onlyMsg := cfgBool(cfg, "json_parse_body", false), cfgBool(cfg, "only_message", false)
	msgs := make(chan mqtt.Message, 64)
	tok := c.SubscribeMultiple(topics, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case msgs <- m:
		case <-ctx.Done():
		}
	})
	if !tok.WaitTimeout(mqttTimeout) {
		return fmt.Errorf("trigger.mqtt: subscribe timed out")
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("trigger.mqtt: subscribe: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-lost:
			return fmt.Errorf("trigger.mqtt: connection lost: %w", err)
		case m := <-msgs:
			msg := parseMessageBody(m.Payload(), jsonParse)
			if onlyMsg {
				if obj, ok := msg.(map[string]interface{}); ok {
					emit(workflow.NewItem(obj))
				} else {
					emit(workflow.NewItem(map[string]interface{}{"message": msg}))
				}
				continue
			}
			emit(workflow.NewItem(map[string]interface{}{"message": msg, "topic": m.Topic()}))
		}
	}
}
