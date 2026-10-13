package dbnodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/monoes/mono-agent/internal/workflow"
)

func runRedis(t *testing.T, mr *miniredis.Miniredis, input workflow.NodeInput, cfg map[string]interface{}) []workflow.Item {
	t.Helper()
	base := map[string]interface{}{"connection_string": "redis://" + mr.Addr()}
	for k, v := range cfg {
		base[k] = v
	}
	out, err := (&RedisNode{}).Execute(context.Background(), input, base)
	if err != nil {
		t.Fatal(err)
	}
	return mainItems(t, out)
}

func TestRedisLegacyGetUnchanged(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Set("k", "v")
	got := runRedis(t, mr, items(map[string]interface{}{"a": 1}, map[string]interface{}{"a": 2}), map[string]interface{}{"operation": "get", "key": "k"})
	if len(got) != 1 || !reflect.DeepEqual(got[0].JSON, map[string]interface{}{"key": "k", "value": "v", "exists": true}) {
		t.Fatalf("legacy get = %+v", got)
	}
}

func TestRedisN8nSetGetPerItem(t *testing.T) {
	mr := miniredis.RunT(t)
	in := items(map[string]interface{}{"id": "a", "v": "1"}, map[string]interface{}{"id": "b", "v": "2"})
	got := runRedis(t, mr, in, map[string]interface{}{
		"operation": "set", "key": "user:{{ $json.id }}", "value": "{{ $json.v }}", "expire": true, "ttl": float64(60),
	})
	if len(got) != 2 || got[0].JSON["id"] != "a" {
		t.Fatalf("set should pass items through: %+v", got)
	}
	if v, _ := mr.Get("user:b"); v != "2" {
		t.Errorf("user:b = %q", v)
	}
	if ttl := mr.TTL("user:a"); ttl.Seconds() != 60 {
		t.Errorf("ttl = %v", ttl)
	}
	got = runRedis(t, mr, items(map[string]interface{}{"id": "a"}), map[string]interface{}{
		"operation": "get", "key": "user:{{ $json.id }}", "property_name": "out.value",
	})
	if !reflect.DeepEqual(got[0].JSON, map[string]interface{}{"out": map[string]interface{}{"value": "1"}}) {
		t.Errorf("get = %+v", got[0].JSON)
	}
}

func TestRedisN8nHashListAndKeys(t *testing.T) {
	mr := miniredis.RunT(t)
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "set", "key": "h", "key_type": "hash", "value": `{"a": 1, "b": "x"}`})
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "set", "key": "l", "value": []interface{}{"x", "y"}, "n8n_compat": true})
	got := runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "keys", "key_pattern": "*"})
	want := map[string]interface{}{
		"h": map[string]interface{}{"a": "1", "b": "x"},
		"l": []interface{}{"x", "y"},
	}
	if !reflect.DeepEqual(got[0].JSON, want) {
		t.Errorf("keys = %#v", got[0].JSON)
	}
	got = runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "keys", "key_pattern": "h*", "get_values": false})
	if !reflect.DeepEqual(got[0].JSON["keys"], []interface{}{"h"}) {
		t.Errorf("keys only = %v", got[0].JSON)
	}
}

func TestRedisN8nPushPopLlenIncrPublishDeleteInfo(t *testing.T) {
	mr := miniredis.RunT(t)
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "push", "list": "q", "message_data": `{"n":1}`})
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "push", "list": "q", "message_data": "tail", "tail": true})
	got := runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "llen", "list": "q"})
	if got[0].JSON["q"] != int64(2) {
		t.Errorf("llen = %v", got[0].JSON)
	}
	got = runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "pop", "list": "q", "property_name": "msg"})
	if !reflect.DeepEqual(got[0].JSON["msg"], map[string]interface{}{"n": float64(1)}) {
		t.Errorf("pop head = %v", got[0].JSON)
	}
	got = runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "pop", "list": "q", "tail": true})
	if got[0].JSON["propertyName"] != "tail" {
		t.Errorf("pop tail = %v", got[0].JSON)
	}
	got = runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "incr", "key": "c", "n8n_compat": true})
	if got[0].JSON["c"] != int64(1) {
		t.Errorf("incr = %v", got[0].JSON)
	}
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "publish", "channel": "ch", "message_data": "hi"})
	runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "delete", "key": "c"})
	if mr.Exists("c") {
		t.Error("delete did not remove the key")
	}
	got = runRedis(t, mr, workflow.NodeInput{}, map[string]interface{}{"operation": "info"})
	if len(got) != 1 || len(got[0].JSON) == 0 {
		t.Errorf("info = %v", got)
	}
}

func TestParseRedisInfo(t *testing.T) {
	got := parseRedisInfo("# Server\r\nredis_version:7.2.4\r\nuptime_in_seconds:42\r\ndb0:keys=3,expires=0\r\n")
	want := map[string]interface{}{
		"redis_version":     "7.2.4",
		"uptime_in_seconds": float64(42),
		"db0":               map[string]interface{}{"keys": float64(3), "expires": float64(0)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}
