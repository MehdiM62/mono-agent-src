package dbnodes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/monoes/mono-agent/internal/workflow"
)

// queuePerItemFields are resolved once per input item for every queue node.
var queuePerItemFields = []string{"message", "message_key", "headers", "topic", "queue", "routing_key", "sink", "header_parameters", "send_only_property"}

// tlsFromConfig builds a TLS config from the shared credential keys
// (ssl / ca / cert / key / allow_unauthorized_certs). nil means plain TCP.
func tlsFromConfig(cfg map[string]interface{}, sslDefault bool) (*tls.Config, error) {
	if !cfgBool(cfg, "ssl", sslDefault) {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfgBool(cfg, "allow_unauthorized_certs", false) || (cfg["reject_unauthorized"] != nil && !cfgBool(cfg, "reject_unauthorized", true)) {
		tc.InsecureSkipVerify = true //nolint:gosec // explicit user opt-in for self-signed brokers
	}
	if ca := cfgString(cfg, "ca"); ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("'ca' is not a PEM certificate")
		}
		tc.RootCAs = pool
	}
	cert, key := cfgString(cfg, "cert"), cfgString(cfg, "key")
	if cert != "" || key != "" {
		pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
		if err != nil {
			return nil, fmt.Errorf("'cert'/'key': %w", err)
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	return tc, nil
}

// kafkaClientOpts turns the Kafka credential keys into client options.
func kafkaClientOpts(cfg map[string]interface{}) ([]kgo.Opt, error) {
	brokers := toStringList(cfg["brokers"])
	if len(brokers) == 0 {
		return nil, fmt.Errorf("'brokers' is required (comma-separated host:port list)")
	}
	opts := []kgo.Opt{kgo.SeedBrokers(brokers...)}
	if id := cfgString(cfg, "client_id"); id != "" {
		opts = append(opts, kgo.ClientID(id))
	}
	tc, err := tlsFromConfig(cfg, false)
	if err != nil {
		return nil, err
	}
	if tc != nil {
		opts = append(opts, kgo.DialTLSConfig(tc))
	}
	if cfgBool(cfg, "authentication", false) || cfgString(cfg, "username") != "" {
		user, pass := cfgString(cfg, "username"), cfgString(cfg, "password")
		switch strings.ToLower(cfgString(cfg, "sasl_mechanism")) {
		case "", "plain":
			opts = append(opts, kgo.SASL(plain.Auth{User: user, Pass: pass}.AsMechanism()))
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(scram.Auth{User: user, Pass: pass}.AsSha256Mechanism()))
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(scram.Auth{User: user, Pass: pass}.AsSha512Mechanism()))
		default:
			return nil, fmt.Errorf("'sasl_mechanism' must be plain, scram-sha-256 or scram-sha-512")
		}
	}
	return opts, nil
}

// KafkaNode produces one record per input item.
// Type: "queue.kafka"
type KafkaNode struct{}

func (n *KafkaNode) Type() string                  { return "queue.kafka" }
func (n *KafkaNode) PerItemConfigFields() []string { return queuePerItemFields }

func (n *KafkaNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	opts, err := kafkaClientOpts(config)
	if err != nil {
		return nil, fmt.Errorf("queue.kafka: %w", err)
	}
	if cfgBool(config, "acks", false) {
		opts = append(opts, kgo.RequiredAcks(kgo.AllISRAcks()))
	} else {
		opts = append(opts, kgo.RequiredAcks(kgo.LeaderAck()), kgo.DisableIdempotentWrite())
	}
	switch strings.ToLower(cfgString(config, "compression")) {
	case "", "none":
	case "gzip":
		opts = append(opts, kgo.ProducerBatchCompression(kgo.GzipCompression()))
	case "snappy":
		opts = append(opts, kgo.ProducerBatchCompression(kgo.SnappyCompression()))
	case "lz4":
		opts = append(opts, kgo.ProducerBatchCompression(kgo.Lz4Compression()))
	case "zstd":
		opts = append(opts, kgo.ProducerBatchCompression(kgo.ZstdCompression()))
	default:
		return nil, fmt.Errorf("queue.kafka: 'compression' must be none, gzip, snappy, lz4 or zstd")
	}
	if ms, ok := cfgInt(config, "timeout"); ok && ms > 0 {
		opts = append(opts, kgo.ProduceRequestTimeout(time.Duration(ms)*time.Millisecond))
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("queue.kafka: %w", err)
	}
	defer cl.Close()

	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, queuePerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("queue.kafka: %w", err)
		}
		rec, err := kafkaRecord(cfg, item)
		if err != nil {
			return nil, fmt.Errorf("queue.kafka (item %d): %w", i, err)
		}
		r, err := cl.ProduceSync(ctx, rec).First()
		if err != nil {
			return nil, fmt.Errorf("queue.kafka (item %d): produce failed: %w", i, err)
		}
		out = append(out, workflow.NewItem(map[string]interface{}{
			"success": true, "topic": r.Topic, "partition": r.Partition, "offset": r.Offset,
		}))
	}
	return mainOutputs(out, nil), nil
}

func kafkaRecord(cfg map[string]interface{}, item workflow.Item) (*kgo.Record, error) {
	topic := cfgString(cfg, "topic")
	if topic == "" {
		return nil, fmt.Errorf("'topic' is required")
	}
	val, err := messagePayload(item, cfgBool(cfg, "send_input_data", true), cfg["message"])
	if err != nil {
		return nil, err
	}
	rec := &kgo.Record{Topic: topic, Value: val}
	if k := cfgString(cfg, "message_key"); k != "" {
		rec.Key = []byte(k)
	}
	headers, err := toMap(cfg["headers"])
	if err != nil {
		return nil, fmt.Errorf("'headers': %w", err)
	}
	for _, k := range sortedMapKeys(headers) {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: k, Value: []byte(redisString(headers[k]))})
	}
	return rec, nil
}

// SubscribeKafka consumes 'topic' as consumer group 'group_id' and calls emit
// once per record, committing the record after emit returns. It blocks until
// ctx is cancelled or the client fails.
// Keys: brokers, client_id, ssl…, topic, group_id, from_beginning (default
// true), json_parse_message, only_message, return_headers.
func SubscribeKafka(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	topic, group := cfgString(cfg, "topic"), cfgString(cfg, "group_id")
	if topic == "" || group == "" {
		return fmt.Errorf("trigger.kafka: 'topic' and 'group_id' are required")
	}
	opts, err := kafkaClientOpts(cfg)
	if err != nil {
		return fmt.Errorf("trigger.kafka: %w", err)
	}
	reset := kgo.NewOffset().AtEnd()
	if cfgBool(cfg, "from_beginning", true) {
		reset = kgo.NewOffset().AtStart()
	}
	opts = append(opts, kgo.ConsumeTopics(topic), kgo.ConsumerGroup(group), kgo.ConsumeResetOffset(reset), kgo.DisableAutoCommit())
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("trigger.kafka: %w", err)
	}
	defer cl.Close()
	jsonParse, onlyMsg, withHeaders := cfgBool(cfg, "json_parse_message", false), cfgBool(cfg, "only_message", false), cfgBool(cfg, "return_headers", false)
	for {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("trigger.kafka: fetch: %w", errs[0].Err)
		}
		var done []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			emit(workflow.NewItem(kafkaItem(r, jsonParse, onlyMsg, withHeaders)))
			done = append(done, r)
		})
		if len(done) > 0 {
			if err := cl.CommitRecords(ctx, done...); err != nil && ctx.Err() == nil {
				return fmt.Errorf("trigger.kafka: commit: %w", err)
			}
		}
	}
}

func kafkaItem(r *kgo.Record, jsonParse, onlyMsg, withHeaders bool) map[string]interface{} {
	msg := parseMessageBody(r.Value, jsonParse)
	if onlyMsg && jsonParse {
		if m, ok := msg.(map[string]interface{}); ok {
			return m
		}
	}
	data := map[string]interface{}{"message": msg, "topic": r.Topic, "partition": r.Partition, "offset": r.Offset}
	if onlyMsg {
		data = map[string]interface{}{"message": msg}
	}
	if r.Key != nil && !onlyMsg {
		data["key"] = string(r.Key)
	}
	if withHeaders && len(r.Headers) > 0 {
		h := map[string]interface{}{}
		for _, hd := range r.Headers {
			h[hd.Key] = string(hd.Value)
		}
		data["headers"] = h
	}
	return data
}
