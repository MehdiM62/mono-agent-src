package image

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// Typed parsing of image.edit's map[string]interface{} config. Values come
// from workflow JSON (numbers are float64), from Go callers and tests (int),
// or from text boxes (numeric strings); every accessor accepts all three and
// reports a bad value with the config key it came from.

func cfgStr(m map[string]interface{}, key, def string) string {
	switch v := m[key].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return v
		}
	case nil:
	default:
		return fmt.Sprintf("%v", v)
	}
	return def
}

func cfgNum(m map[string]interface{}, key string, def float64) (float64, error) {
	switch v := m[key].(type) {
	case nil:
		return def, nil
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not a number", key, v)
		}
		return f, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return def, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not a number", key, v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("%s: %v (%T) is not a number", key, v, v)
	}
}

func cfgInt(m map[string]interface{}, key string, def int) (int, error) {
	f, err := cfgNum(m, key, float64(def))
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1<<30 {
		return 0, fmt.Errorf("%s: %v is out of range", key, f)
	}
	return int(math.Round(f)), nil
}

// hexAlphaMode says how the last byte of an 8-digit (or 4-digit) hex color
// is read. "alpha" is the CSS convention (ff = opaque). "opacity" is the
// GraphicsMagick convention n8n's Edit Image node inherits (00 = opaque,
// ff = fully transparent), which imported n8n workflows need.
type hexAlphaMode bool

const (
	hexAlpha   hexAlphaMode = false
	hexOpacity hexAlphaMode = true
)

var namedColors = map[string]color.NRGBA{
	"transparent": {},
	"none":        {},
	"black":       {0, 0, 0, 255},
	"white":       {255, 255, 255, 255},
	"red":         {255, 0, 0, 255},
	"green":       {0, 128, 0, 255},
	"lime":        {0, 255, 0, 255},
	"blue":        {0, 0, 255, 255},
	"yellow":      {255, 255, 0, 255},
	"cyan":        {0, 255, 255, 255},
	"magenta":     {255, 0, 255, 255},
	"gray":        {128, 128, 128, 255},
	"grey":        {128, 128, 128, 255},
	"orange":      {255, 165, 0, 255},
	"purple":      {128, 0, 128, 255},
}

// parseColor reads #rgb, #rgba, #rrggbb, #rrggbbaa, rgb(r,g,b),
// rgba(r,g,b,a) (a in 0..1) or a basic color name.
func parseColor(s string, mode hexAlphaMode) (color.NRGBA, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := namedColors[s]; ok {
		return c, nil
	}
	if strings.HasPrefix(s, "rgb") {
		return parseRGBFunc(s)
	}
	hex := strings.TrimPrefix(s, "#")
	if len(hex) == 3 || len(hex) == 4 {
		var b strings.Builder
		for _, r := range hex {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		hex = b.String()
	}
	if len(hex) != 6 && len(hex) != 8 {
		return color.NRGBA{}, fmt.Errorf("%q is not a color (use #rrggbb, #rrggbbaa, rgb()/rgba() or a color name)", s)
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("%q is not a hex color", s)
	}
	if len(hex) == 6 {
		return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, nil
	}
	a := uint8(v)
	if mode == hexOpacity {
		a = 255 - a
	}
	return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), a}, nil
}

func parseRGBFunc(s string) (color.NRGBA, error) {
	open, end := strings.Index(s, "("), strings.LastIndex(s, ")")
	if open < 0 || end < open {
		return color.NRGBA{}, fmt.Errorf("%q is not a color", s)
	}
	parts := strings.Split(s[open+1:end], ",")
	if len(parts) != 3 && len(parts) != 4 {
		return color.NRGBA{}, fmt.Errorf("%q needs 3 or 4 components", s)
	}
	out := color.NRGBA{A: 255}
	ch := []*uint8{&out.R, &out.G, &out.B}
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return color.NRGBA{}, fmt.Errorf("%q: component %d is not a number", s, i+1)
		}
		if i == 3 {
			out.A = uint8(math.Round(clampF(f, 0, 1) * 255))
			continue
		}
		*ch[i] = uint8(math.Round(clampF(f, 0, 255)))
	}
	return out, nil
}

func clampF(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// cfgColor reads a color option. A missing value yields def unchanged, so
// defaults are not reinterpreted by the hex alpha mode.
func cfgColor(m map[string]interface{}, key string, def color.NRGBA, mode hexAlphaMode) (color.NRGBA, error) {
	s := cfgStr(m, key, "")
	if s == "" {
		return def, nil
	}
	c, err := parseColor(s, mode)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("%s: %w", key, err)
	}
	return c, nil
}

// cfgChoice reads a select option, matching case-insensitively against the
// allowed values and returning the canonical spelling.
func cfgChoice(m map[string]interface{}, key, def string, allowed ...string) (string, error) {
	v := strings.TrimSpace(cfgStr(m, key, def))
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return a, nil
		}
	}
	return "", fmt.Errorf("%s: %q is not one of %s", key, v, strings.Join(allowed, ", "))
}

// cfgSteps reads the multi_step operations list: a list of objects, a JSON
// string holding one, or n8n's fixedCollection shape {"operations": [...]}.
func cfgSteps(m map[string]interface{}, key string) ([]map[string]interface{}, error) {
	raw := m[key]
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("%s: at least one step is required", key)
		}
		var v interface{}
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("%s: not valid JSON: %w", key, err)
		}
		raw = v
	}
	if obj, ok := raw.(map[string]interface{}); ok {
		raw = obj["operations"]
	}
	var list []interface{}
	switch v := raw.(type) {
	case []interface{}:
		list = v
	case []map[string]interface{}:
		for _, e := range v {
			list = append(list, e)
		}
	case nil:
	default:
		return nil, fmt.Errorf("%s: want a list of steps, got %T", key, raw)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%s: at least one step is required", key)
	}
	out := make([]map[string]interface{}, len(list))
	for i, e := range list {
		step, ok := e.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s[%d]: want an object with an \"operation\" key, got %T", key, i, e)
		}
		out[i] = normalizeStepKeys(step)
	}
	return out, nil
}

// normalizeStepKeys renames n8n camelCase step keys (as found in an n8n
// multiStep collection) to this node's snake_case keys and n8n operation
// values to ours, so pasted n8n steps work unchanged.
func normalizeStepKeys(step map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(step))
	for k, v := range step {
		if ours, ok := N8nEditImageParams[k]; ok && !strings.Contains(k, ".") {
			if _, taken := step[ours]; !taken || ours == k {
				k = ours
			}
		}
		out[k] = v
	}
	if op, ok := out["operation"].(string); ok {
		if ours, ok := N8nEditImageOperations[op]; ok {
			out["operation"] = ours
		}
	}
	return out
}
