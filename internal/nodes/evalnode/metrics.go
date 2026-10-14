package evalnode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Default metric names, as n8n names them.
var defaultMetricNames = map[string]string{
	"correctness":      "Correctness",
	"helpfulness":      "Helpfulness",
	"categorization":   "Categorization",
	"stringSimilarity": "String similarity",
	"toolsUsed":        "Tools Used",
}

// text is a metric field as text, trimmed: what n8n's toString().trim()
// gives for a string, a number or a boolean.
func text(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool, int, int64:
		return fmt.Sprint(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSpace(string(b))
}

// answers returns the expected and actual answers, both required.
func answers(cfg map[string]interface{}) (expected, actual string, err error) {
	expected, actual = text(cfg["expected_answer"]), text(cfg["actual_answer"])
	if expected == "" {
		return "", "", fmt.Errorf("%w: expected answer is missing: fill in expected_answer", workflow.ErrInvalidConfig)
	}
	if actual == "" {
		return "", "", fmt.Errorf("%w: actual answer is missing: fill in actual_answer", workflow.ErrInvalidConfig)
	}
	return expected, actual, nil
}

// categorization is 1 when the answers are the same text, else 0.
func categorization(expected, actual string) float64 {
	if expected == actual {
		return 1
	}
	return 0
}

// stringSimilarity is 1 minus the edit distance over the longer length,
// counted in UTF-16 code units as n8n counts them.
func stringSimilarity(expected, actual string) float64 {
	a, b := utf16.Encode([]rune(expected)), utf16.Encode([]rune(actual))
	longer := max(len(a), len(b))
	if longer == 0 {
		return 1
	}
	return 1 - float64(levenshtein(a, b))/float64(longer)
}

func levenshtein(a, b []uint16) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// toolNameRe matches what n8n replaces in a node name to get its tool name.
var toolNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// toolName is the tool name an agent calls a node by: runs of other
// characters become "_", at most 64 characters.
func toolName(name string) string {
	s := toolNameRe.ReplaceAllString(name, "_")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "_-")
	}
	return s
}

// toolsUsed is the share of the expected tools (comma-separated node or
// tool names) the agent's intermediate steps called.
func toolsUsed(expected, steps interface{}) (float64, error) {
	var want []string
	for _, t := range strings.Split(text(expected), ",") {
		if t = strings.TrimSpace(t); t != "" {
			want = append(want, strings.ToLower(toolName(t)))
		}
	}
	if len(want) == 0 {
		return 0, fmt.Errorf("%w: expected tool name missing: list at least one in expected_tools (comma-separated)", workflow.ErrInvalidConfig)
	}
	if s, ok := steps.(string); ok {
		if err := json.Unmarshal([]byte(s), &steps); err != nil {
			steps = nil
		}
	}
	list, ok := steps.([]interface{})
	if !ok {
		return 0, fmt.Errorf("%w: intermediate steps missing: map the agent's intermediateSteps into intermediate_steps", workflow.ErrInvalidConfig)
	}
	used := 0
	for _, w := range want {
		for _, st := range list {
			m, _ := st.(map[string]interface{})
			action, _ := m["action"].(map[string]interface{})
			if tool, ok := action["tool"].(string); ok && strings.ToLower(tool) == w {
				used++
				break
			}
		}
	}
	return float64(used) / float64(len(want)), nil
}

// customMetrics reads [{name, value}]: every value must be a number (or
// text of one) and every metric named.
func customMetrics(v interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	list, err := pairs(v, "metrics")
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		f, ok := number(p.value)
		if !ok {
			return nil, fmt.Errorf("%w: value for %q isn't a number (it is %v); metrics must be numeric", workflow.ErrInvalidConfig, p.name, p.value)
		}
		if p.name == "" {
			return nil, fmt.Errorf("%w: metric name missing: name each metric", workflow.ErrInvalidConfig)
		}
		out[p.name] = f
	}
	return out, nil
}

func number(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, true
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	case nil:
		return 0, true
	}
	return 0, false
}

// pair is one {name, value} entry of inputs, outputs or metrics.
type pair struct {
	name  string
	value interface{}
}

// pairs reads a [{name, value}] list, or its JSON text.
func pairs(v interface{}, field string) ([]pair, error) {
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("%w: %s must be a list of {name, value}: %v", workflow.ErrInvalidConfig, field, err)
		}
	}
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: %s must be a list of {name, value}", workflow.ErrInvalidConfig, field)
	}
	out := make([]pair, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be {name, value}", workflow.ErrInvalidConfig, field, i)
		}
		name, _ := m["name"].(string)
		out = append(out, pair{name: strings.TrimSpace(name), value: m["value"]})
	}
	return out, nil
}
