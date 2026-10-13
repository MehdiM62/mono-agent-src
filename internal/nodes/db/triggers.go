package dbnodes

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/monoes/mono-agent/internal/workflow"
)

// SubscribeFunc runs one long-lived consumer: it calls emit once per message
// and blocks until ctx is cancelled (returning nil) or the connection fails
// (returning the error).
type SubscribeFunc func(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error

// TriggerSubscribers maps each trigger node type to its consumer.
var TriggerSubscribers = map[string]SubscribeFunc{
	"trigger.kafka":    SubscribeKafka,
	"trigger.rabbitmq": SubscribeRabbitMQ,
	"trigger.amqp":     SubscribeAMQP,
	"trigger.mqtt":     SubscribeMQTT,
	"trigger.redis":    SubscribeRedis,
	"trigger.postgres": SubscribePostgres,
}

// CredentialResolver returns cfg with the connection's fields merged in
// (what RunExecution does for credential_id before Execute). nil = use cfg
// as it is.
type CredentialResolver func(ctx context.Context, cfg map[string]interface{}) (map[string]interface{}, error)

// triggerSource adapts a SubscribeFunc to workflow.TriggerSource: Activate
// starts the consumer in a goroutine that restarts it with backoff after a
// failure, and the returned stop func cancels it and waits for it to exit.
type triggerSource struct {
	nodeType string
	sub      SubscribeFunc
	resolve  CredentialResolver
	minWait  time.Duration
	maxWait  time.Duration
	onError  func(nodeType string, err error)
}

// NewTriggerSource returns the workflow.TriggerSource for one of the
// TriggerSubscribers types. onError (optional) is told about every consumer
// failure before the restart.
func NewTriggerSource(nodeType string, resolve CredentialResolver, onError func(nodeType string, err error)) (workflow.TriggerSource, error) {
	sub, ok := TriggerSubscribers[nodeType]
	if !ok {
		return nil, fmt.Errorf("no subscriber for trigger type %q", nodeType)
	}
	return &triggerSource{nodeType: nodeType, sub: sub, resolve: resolve, minWait: time.Second, maxWait: time.Minute, onError: onError}, nil
}

func (s *triggerSource) Activate(_ *workflow.Workflow, node *workflow.WorkflowNode, fire func(items []workflow.Item)) (func(), error) {
	if node.Config == nil {
		if err := node.ParseConfig(); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	cfg := node.Config
	ctx, cancel := context.WithCancel(context.Background())
	if s.resolve != nil {
		resolved, err := s.resolve(ctx, cfg)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("%s: credentials: %w", s.nodeType, err)
		}
		cfg = resolved
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.loop(ctx, cfg, func(it workflow.Item) { fire([]workflow.Item{it}) })
	}()
	return func() { cancel(); wg.Wait() }, nil
}

func (s *triggerSource) loop(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) {
	wait := s.minWait
	for ctx.Err() == nil {
		start := time.Now()
		err := s.sub(ctx, cfg, emit)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = fmt.Errorf("consumer stopped")
		}
		if s.onError != nil {
			s.onError(s.nodeType, err)
		}
		if time.Since(start) > s.maxWait {
			wait = s.minWait // it ran fine for a while: restart quickly
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait *= 2; wait > s.maxWait {
			wait = s.maxWait
		}
	}
}

// SubscribeRedis subscribes to 'channels' (comma-separated; names with
// glob characters use PSUBSCRIBE) and calls emit per message.
// Keys: connection_string, channels, json_parse_body, only_message.
func SubscribeRedis(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	opts, err := redis.ParseURL(cfgString(cfg, "connection_string"))
	if err != nil {
		return fmt.Errorf("trigger.redis: invalid connection_string: %w", err)
	}
	channels := toStringList(cfg["channels"])
	if len(channels) == 0 {
		return fmt.Errorf("trigger.redis: 'channels' is required")
	}
	var plain, patterns []string
	for _, c := range channels {
		if strings.ContainsAny(c, "*?[") {
			patterns = append(patterns, c)
		} else {
			plain = append(plain, c)
		}
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	ps := rdb.Subscribe(ctx)
	defer ps.Close()
	if len(plain) > 0 {
		if err := ps.Subscribe(ctx, plain...); err != nil {
			return fmt.Errorf("trigger.redis: subscribe: %w", err)
		}
	}
	if len(patterns) > 0 {
		if err := ps.PSubscribe(ctx, patterns...); err != nil {
			return fmt.Errorf("trigger.redis: psubscribe: %w", err)
		}
	}
	jsonParse, onlyMsg := cfgBool(cfg, "json_parse_body", false), cfgBool(cfg, "only_message", false)
	// Confirm the subscription before handing over to the message channel,
	// which (unlike ReceiveMessage) can be abandoned as soon as ctx ends.
	if _, err := ps.Receive(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("trigger.redis: subscribe: %w", err)
	}
	msgs := ps.Channel()
	for {
		var msg *redis.Message
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-msgs:
			if !ok {
				return fmt.Errorf("trigger.redis: subscription closed")
			}
			msg = m
		}
		body := parseMessageBody([]byte(msg.Payload), jsonParse)
		if onlyMsg {
			if m, ok := body.(map[string]interface{}); ok {
				emit(workflow.NewItem(m))
			} else {
				emit(workflow.NewItem(map[string]interface{}{"message": body}))
			}
			continue
		}
		emit(workflow.NewItem(map[string]interface{}{"channel": msg.Channel, "message": body}))
	}
}
