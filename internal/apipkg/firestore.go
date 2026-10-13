package apipkg

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// firestoreValue encodes a JSON value as a Firestore Value.
func firestoreValue(v any) map[string]any {
	switch t := v.(type) {
	case nil:
		return map[string]any{"nullValue": nil}
	case bool:
		return map[string]any{"booleanValue": t}
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return map[string]any{"integerValue": strconv.FormatInt(int64(t), 10)}
		}
		return map[string]any{"doubleValue": t}
	case int64:
		return map[string]any{"integerValue": strconv.FormatInt(t, 10)}
	case int:
		return map[string]any{"integerValue": strconv.Itoa(t)}
	case string:
		switch t {
		case "true":
			return map[string]any{"booleanValue": true}
		case "false":
			return map[string]any{"booleanValue": false}
		}
		if _, err := time.Parse(time.RFC3339, t); err == nil {
			return map[string]any{"timestampValue": t}
		}
		return map[string]any{"stringValue": t}
	case []any:
		vals := make([]any, len(t))
		for i, el := range t {
			vals[i] = firestoreValue(el)
		}
		return map[string]any{"arrayValue": map[string]any{"values": vals}}
	case map[string]any:
		return map[string]any{"mapValue": map[string]any{"fields": firestoreFields(t, nil)}}
	}
	return map[string]any{"stringValue": fmt.Sprint(v)}
}

// firestoreFields encodes an object's keys (all, or only cols, missing
// ones as nullValue) as Firestore fields.
func firestoreFields(obj map[string]any, cols []string) map[string]any {
	out := map[string]any{}
	if cols == nil {
		for k, v := range obj {
			out[k] = firestoreValue(v)
		}
		return out
	}
	for _, c := range cols {
		out[c] = firestoreValue(obj[c])
	}
	return out
}

// firestoreDecode turns a Firestore Value back into plain JSON.
func firestoreDecode(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for k, x := range m {
		switch k {
		case "nullValue":
			return nil
		case "booleanValue", "doubleValue", "stringValue", "timestampValue", "referenceValue", "bytesValue":
			return x
		case "integerValue":
			if n, err := strconv.ParseInt(stringify(x), 10, 64); err == nil {
				return float64(n)
			}
			return x
		case "geoPointValue":
			return x
		case "arrayValue":
			vals, _ := lookupPath(x, []string{"values"}).([]any)
			out := make([]any, len(vals))
			for i, el := range vals {
				out[i] = firestoreDecode(el)
			}
			return out
		case "mapValue":
			fields, _ := lookupPath(x, []string{"fields"}).(map[string]any)
			out := map[string]any{}
			for fk, fv := range fields {
				out[fk] = firestoreDecode(fv)
			}
			return out
		}
	}
	return v
}

// firestoreDoc turns a Firestore document into n8n's simple shape:
// {_name, _id, _createTime, _updateTime, ...fields}. Non-documents pass.
func firestoreDoc(el any) any {
	doc, ok := el.(map[string]any)
	if !ok {
		return el
	}
	name, hasName := doc["name"].(string)
	fields, hasFields := doc["fields"].(map[string]any)
	if !hasName && !hasFields {
		return el
	}
	out := map[string]any{}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = firestoreDecode(fields[k])
	}
	if hasName {
		out["_name"] = name
		out["_id"] = name[strings.LastIndex(name, "/")+1:]
	}
	if t, ok := doc["createTime"]; ok {
		out["_createTime"] = t
	}
	if t, ok := doc["updateTime"]; ok {
		out["_updateTime"] = t
	}
	return out
}
