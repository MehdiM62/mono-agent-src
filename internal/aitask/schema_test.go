package aitask

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateReportsThePathOfTheFirstProblem(t *testing.T) {
	schema := map[string]any{
		"type": "object", "required": []any{"items"}, "additionalProperties": false,
		"properties": map[string]any{
			"items": map[string]any{"type": "array", "minItems": 1.0, "items": map[string]any{
				"type": "object", "required": []any{"n"},
				"properties": map[string]any{"n": map[string]any{"type": "integer"}, "k": map[string]any{"enum": []any{"a", "b"}}},
			}},
		},
	}
	cases := map[string]string{
		`{"items":[{"n":1}]}`:              "",
		`{}`:                               `missing required property "items"`,
		`{"items":[]}`:                     "$.items: must have at least 1 items",
		`{"items":[{"n":1.5}]}`:            "$.items[0].n: want integer",
		`{"items":[{"n":1,"k":"z"}]}`:      "$.items[0].k: must be one of",
		`{"items":[{"n":1}],"extra":true}`: `unexpected property "extra"`,
		`[]`:                               "$: want object, got array",
	}
	for doc, want := range cases {
		var v any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatal(err)
		}
		err := Validate(schema, v)
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: err = %v, want %q", doc, err, want)
		}
	}
}

func TestValidateHonoursUpperBounds(t *testing.T) {
	schema := map[string]any{
		"type": "object", "maxProperties": 2.0,
		"properties": map[string]any{
			"s": map[string]any{"type": "string", "maxLength": 3.0},
			"a": map[string]any{"type": "array", "maxItems": 2.0},
		},
	}
	cases := map[string]string{
		`{"s":"abc","a":[1,2]}`:  "",
		`{"s":"héé"}`:            "", // runes, not bytes
		`{"s":"abcd"}`:           "$.s: must be at most 3 characters",
		`{"a":[1,2,3]}`:          "$.a: must have at most 2 items",
		`{"s":"a","a":[],"z":1}`: "must have at most 2 properties",
	}
	for doc, want := range cases {
		var v any
		_ = json.Unmarshal([]byte(doc), &v)
		err := Validate(schema, v)
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: err=%v, want %q", doc, err, want)
		}
	}
}
