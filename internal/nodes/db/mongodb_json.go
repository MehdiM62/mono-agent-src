package dbnodes

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func lookupPath(m map[string]interface{}, path string, dotted bool) (interface{}, bool) {
	if !dotted {
		v, ok := m[path]
		return v, ok
	}
	var cur interface{} = m
	for _, p := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func setPath(m map[string]interface{}, path string, v interface{}) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

// mongoToJSON converts BSON values into plain JSON-friendly Go values.
func mongoToJSON(v interface{}) interface{} {
	switch t := v.(type) {
	case bson.D:
		m := make(map[string]interface{}, len(t))
		for _, e := range t {
			m[e.Key] = mongoToJSON(e.Value)
		}
		return m
	case bson.M:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = mongoToJSON(e)
		}
		return m
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = mongoToJSON(e)
		}
		return m
	case bson.A:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = mongoToJSON(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = mongoToJSON(e)
		}
		return out
	case bson.ObjectID:
		return t.Hex()
	case bson.DateTime:
		return t.Time().UTC().Format(time.RFC3339Nano)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case bson.Decimal128:
		return t.String()
	}
	return v
}
