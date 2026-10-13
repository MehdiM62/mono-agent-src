package dbnodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/monoes/mono-agent/internal/workflow"
)

// redisPerItemFields are resolved once per input item (n8n evaluates every
// Redis parameter per item). Legacy configs resolve them against the first
// item, as the engine did before.
var redisPerItemFields = []string{"key", "value", "field", "property_name", "key_pattern", "list", "channel", "message_data", "ttl", "ttl_seconds"}

var n8nOnlyRedisOps = map[string]bool{"delete": true, "info": true, "llen": true, "pop": true, "publish": true, "push": true}

var n8nRedisKeys = []string{"n8n_compat", "property_name", "key_type", "key_pattern", "get_values", "value_is_json", "expire", "ttl", "list", "message_data", "channel", "tail", "dot_notation"}

func isN8nRedisConfig(config map[string]interface{}) bool {
	if n8nOnlyRedisOps[cfgString(config, "operation")] {
		return true
	}
	for _, k := range n8nRedisKeys {
		if !isZeroConfigValue(config[k]) {
			return true
		}
	}
	return false
}

// executeRedisN8n runs the n8n-compatible Redis operations, once per input
// item (info runs once).
func executeRedisN8n(ctx context.Context, rdb *redis.Client, op string, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if op == "info" {
		raw, err := rdb.Info(ctx).Result()
		if err != nil {
			return nil, fmt.Errorf("db.redis: info failed: %w", err)
		}
		return mainOutputs([]workflow.Item{workflow.NewItem(parseRedisInfo(raw))}, nil), nil
	}
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, redisPerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("db.redis: %w", err)
		}
		res, err := runRedisOp(ctx, rdb, op, cfg, item)
		if err != nil {
			return nil, fmt.Errorf("db.redis: %s (item %d): %w", op, i, err)
		}
		out = append(out, res...)
	}
	return mainOutputs(out, nil), nil
}

func requireKey(cfg map[string]interface{}, key string) (string, error) {
	v := cfgString(cfg, key)
	if v == "" {
		return "", fmt.Errorf("'%s' is required", key)
	}
	return v, nil
}

func runRedisOp(ctx context.Context, rdb *redis.Client, op string, cfg map[string]interface{}, item workflow.Item) ([]workflow.Item, error) {
	passThrough := []workflow.Item{item}
	switch op {
	case "delete":
		key, err := requireKey(cfg, "key")
		if err != nil {
			return nil, err
		}
		return passThrough, rdb.Del(ctx, key).Err()

	case "get":
		key, err := requireKey(cfg, "key")
		if err != nil {
			return nil, err
		}
		v, err := redisGetValue(ctx, rdb, key, cfgString(cfg, "key_type"))
		if err != nil {
			return nil, err
		}
		return []workflow.Item{propertyItem(cfg, v)}, nil

	case "keys":
		pattern := cfgString(cfg, "key_pattern")
		if pattern == "" {
			pattern = cfgString(cfg, "key")
		}
		if pattern == "" {
			return nil, fmt.Errorf("'key_pattern' is required")
		}
		keys, err := redisScan(ctx, rdb, pattern)
		if err != nil {
			return nil, err
		}
		if !cfgBool(cfg, "get_values", true) {
			list := make([]interface{}, len(keys))
			for i, k := range keys {
				list[i] = k
			}
			return []workflow.Item{workflow.NewItem(map[string]interface{}{"keys": list})}, nil
		}
		data := map[string]interface{}{}
		for _, k := range keys {
			if data[k], err = redisGetValue(ctx, rdb, k, ""); err != nil {
				return nil, err
			}
		}
		return []workflow.Item{workflow.NewItem(data)}, nil

	case "llen":
		list, err := requireKey(cfg, "list")
		if err != nil {
			return nil, err
		}
		n, err := rdb.LLen(ctx, list).Result()
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{list: n})}, nil

	case "set":
		key, err := requireKey(cfg, "key")
		if err != nil {
			return nil, err
		}
		if err := redisSetValue(ctx, rdb, key, cfg["value"], cfgString(cfg, "key_type"), cfgBool(cfg, "value_is_json", true)); err != nil {
			return nil, err
		}
		return passThrough, redisExpire(ctx, rdb, key, cfg)

	case "incr":
		key, err := requireKey(cfg, "key")
		if err != nil {
			return nil, err
		}
		n, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		if err := redisExpire(ctx, rdb, key, cfg); err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{key: n})}, nil

	case "publish":
		channel, err := requireKey(cfg, "channel")
		if err != nil {
			return nil, err
		}
		return passThrough, rdb.Publish(ctx, channel, redisString(cfg["message_data"])).Err()

	case "push":
		list, err := requireKey(cfg, "list")
		if err != nil {
			return nil, err
		}
		msg := redisString(cfg["message_data"])
		if cfgBool(cfg, "tail", false) {
			return passThrough, rdb.RPush(ctx, list, msg).Err()
		}
		return passThrough, rdb.LPush(ctx, list, msg).Err()

	case "pop":
		list, err := requireKey(cfg, "list")
		if err != nil {
			return nil, err
		}
		var v string
		if cfgBool(cfg, "tail", false) {
			v, err = rdb.RPop(ctx, list).Result()
		} else {
			v, err = rdb.LPop(ctx, list).Result()
		}
		if err == redis.Nil {
			return []workflow.Item{propertyItem(cfg, nil)}, nil
		}
		if err != nil {
			return nil, err
		}
		return []workflow.Item{propertyItem(cfg, parseMessageBody([]byte(v), true))}, nil
	}
	return nil, fmt.Errorf("unknown operation %q (want delete, get, incr, info, keys, llen, pop, publish, push or set)", op)
}

// propertyItem returns a fresh item holding v under property_name (default
// "propertyName"); with dot_notation (default on) "a.b" nests.
func propertyItem(cfg map[string]interface{}, v interface{}) workflow.Item {
	name := cfgString(cfg, "property_name")
	if name == "" {
		name = "propertyName"
	}
	data := map[string]interface{}{}
	if cfgBool(cfg, "dot_notation", true) && strings.Contains(name, ".") {
		setPath(data, name, v)
	} else {
		data[name] = v
	}
	return workflow.NewItem(data)
}

func redisExpire(ctx context.Context, rdb *redis.Client, key string, cfg map[string]interface{}) error {
	if !cfgBool(cfg, "expire", false) {
		return nil
	}
	ttl, ok := cfgInt(cfg, "ttl")
	if !ok || ttl <= 0 {
		return nil
	}
	return rdb.Expire(ctx, key, time.Duration(ttl)*time.Second).Err()
}

func redisScan(ctx context.Context, rdb *redis.Client, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		batch, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if cursor = next; cursor == 0 {
			return keys, nil
		}
	}
}

// redisGetValue reads key as keyType (automatic asks Redis for the type).
// A missing key yields nil.
func redisGetValue(ctx context.Context, rdb *redis.Client, key, keyType string) (interface{}, error) {
	if keyType == "" || keyType == "automatic" {
		t, err := rdb.Type(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		keyType = t
	}
	switch keyType {
	case "string":
		v, err := rdb.Get(ctx, key).Result()
		if err == redis.Nil {
			return nil, nil
		}
		return v, err
	case "hash":
		m, err := rdb.HGetAll(ctx, key).Result()
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out, err
	case "list":
		l, err := rdb.LRange(ctx, key, 0, -1).Result()
		return stringsToList(l), err
	case "set", "sets":
		l, err := rdb.SMembers(ctx, key).Result()
		return stringsToList(l), err
	case "none":
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported key type %q", keyType)
}

func stringsToList(l []string) []interface{} {
	out := make([]interface{}, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}

// redisSetValue writes value as keyType. automatic picks string for scalars,
// list for arrays and hash for objects. A list or set replaces the old value.
func redisSetValue(ctx context.Context, rdb *redis.Client, key string, value interface{}, keyType string, valueIsJSON bool) error {
	if keyType == "" || keyType == "automatic" {
		switch value.(type) {
		case []interface{}:
			keyType = "list"
		case map[string]interface{}:
			keyType = "hash"
		default:
			keyType = "string"
		}
	}
	switch keyType {
	case "string":
		return rdb.Set(ctx, key, redisString(value), 0).Err()
	case "hash":
		fields := map[string]interface{}{}
		switch v := value.(type) {
		case map[string]interface{}:
			for k, e := range v {
				fields[k] = redisString(e)
			}
		default:
			s := redisString(v)
			var parsed map[string]interface{}
			if valueIsJSON && json.Unmarshal([]byte(s), &parsed) == nil {
				for k, e := range parsed {
					fields[k] = redisString(e)
				}
			} else {
				parts := strings.Fields(s)
				if len(parts)%2 != 0 {
					return fmt.Errorf("hash value must be 'field value' pairs separated by spaces")
				}
				for i := 0; i < len(parts); i += 2 {
					fields[parts[i]] = parts[i+1]
				}
			}
		}
		if len(fields) == 0 {
			return fmt.Errorf("hash value has no fields")
		}
		return rdb.HSet(ctx, key, fields).Err()
	case "list", "set", "sets":
		var elems []interface{}
		switch v := value.(type) {
		case []interface{}:
			for _, e := range v {
				elems = append(elems, redisString(e))
			}
		default:
			s := redisString(v)
			var parsed []interface{}
			if json.Unmarshal([]byte(s), &parsed) == nil {
				for _, e := range parsed {
					elems = append(elems, redisString(e))
				}
			} else {
				elems = append(elems, s)
			}
		}
		pipe := rdb.TxPipeline()
		pipe.Del(ctx, key)
		if len(elems) > 0 {
			if keyType == "list" {
				pipe.RPush(ctx, key, elems...)
			} else {
				pipe.SAdd(ctx, key, elems...)
			}
		}
		_, err := pipe.Exec(ctx)
		return err
	}
	return fmt.Errorf("unsupported key type %q", keyType)
}

// redisString renders a value the way Redis stores it: strings as-is,
// numbers/bools via fmt, objects/arrays as JSON.
func redisString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(t)
		return string(b)
	}
	return fmt.Sprint(v)
}

// parseRedisInfo turns INFO output into an object; "k=v,k=v" values become
// nested objects and numeric values become numbers.
func parseRedisInfo(raw string) map[string]interface{} {
	out := map[string]interface{}{}
	num := func(s string) interface{} {
		if f, err := strconv.ParseFloat(s, 64); err == nil && strings.Trim(s, "0123456789.") == "" {
			return f
		}
		return s
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.Contains(v, "=") {
			sub := map[string]interface{}{}
			for _, pair := range strings.Split(v, ",") {
				if sk, sv, ok := strings.Cut(pair, "="); ok {
					sub[sk] = num(sv)
				}
			}
			out[k] = sub
		} else {
			out[k] = num(v)
		}
	}
	return out
}
