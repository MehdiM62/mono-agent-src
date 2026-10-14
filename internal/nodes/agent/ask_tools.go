package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/workflow"
)

// maxToolResultBytes bounds what one tool call hands back to the model.
const maxToolResultBytes = 32 << 10

// agentTools are the nodes attached to agent.ask through ai_tool
// connections, offered to the agent as caller tools.
type agentTools struct {
	specs  []monomind.ToolSpec
	byName map[string]workflow.SubNode
}

// toolArgRefs find the item fields a tool node's config reads: those are the
// tool's arguments ({{ $json.city }}, $json["city"], index $json "city").
var toolArgRefs = []*regexp.Regexp{
	regexp.MustCompile(`\$json\.([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`\$json\[\s*"([^"]+)"\s*\]`),
	regexp.MustCompile(`index\s+\$json\s+"([^"]+)"`),
}

var toolNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// buildTools turns the attached ai_tool sub-nodes into tool specs; nil when
// none is attached.
func buildTools(input workflow.NodeInput) *agentTools {
	subs := input.SubNodesOf("ai_tool")
	if len(subs) == 0 {
		return nil
	}
	t := &agentTools{byName: map[string]workflow.SubNode{}}
	for _, s := range subs {
		if s.Call == nil {
			continue
		}
		name := strings.Trim(toolNameChars.ReplaceAllString(s.Name, "_"), "_")
		if name == "" {
			name = "tool"
		}
		for base, i := name, 2; t.byName[name].Call != nil; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		t.byName[name] = s
		desc := strings.TrimSpace(configString(s.Config, "tool_description", ""))
		if desc == "" {
			desc = fmt.Sprintf("Runs the %q workflow node (%s) once with the arguments as its input item and returns its output items as JSON.", s.Name, s.Type)
		}
		t.specs = append(t.specs, monomind.ToolSpec{Name: name, Description: desc, Schema: toolSchema(s.Config)})
	}
	if len(t.specs) == 0 {
		return nil
	}
	return t
}

// toolSchema is a tool's argument schema: one string property per item field
// its config reads; other properties are passed on too.
func toolSchema(config map[string]interface{}) map[string]interface{} {
	seen := map[string]bool{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case string:
			for _, re := range toolArgRefs {
				for _, m := range re.FindAllStringSubmatch(x, -1) {
					seen[m[1]] = true
				}
			}
		case map[string]interface{}:
			for _, e := range x {
				walk(e)
			}
		case []interface{}:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(config)
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	props := map[string]interface{}{}
	required := make([]interface{}, 0, len(names))
	for _, k := range names {
		props[k] = map[string]interface{}{"type": "string", "description": "the " + k + " the node reads"}
		required = append(required, k)
	}
	return map[string]interface{}{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

// toolCall is one call an item's turn made, reported on the output item.
type toolCall struct {
	Tool  string                 `json:"tool"`
	Args  map[string]interface{} `json:"args"`
	OK    bool                   `json:"ok"`
	Error string                 `json:"error,omitempty"`
}

type toolCallLog struct {
	mu    sync.Mutex
	calls []toolCall
}

type toolCallKey struct{}

// handler runs the named tool with the model's arguments.
func (at *agentTools) handler(ctx context.Context, log *toolCallLog, name string, raw json.RawMessage) (string, error) {
	sub, ok := at.byName[name]
	if !ok {
		return "", fmt.Errorf("no tool named %q", name)
	}
	args := map[string]interface{}{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("tool %q: arguments must be a JSON object: %v", name, err)
		}
	}
	items, err := sub.Call(ctx, args)
	if log != nil {
		c := toolCall{Tool: name, Args: args, OK: err == nil}
		if err != nil {
			c.Error = err.Error()
		}
		log.mu.Lock()
		log.calls = append(log.calls, c)
		log.mu.Unlock()
	}
	if err != nil {
		return "", err
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it.JSON)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	if len(b) > maxToolResultBytes {
		return string(b[:maxToolResultBytes]) + "…(truncated)", nil
	}
	return string(b), nil
}

func (t *asker) toolSpecs() []monomind.ToolSpec {
	if t.tools == nil {
		return nil
	}
	return t.tools.specs
}

// toolHandler runs tool calls, logging them on the item whose turn ctx is
// (the log travels in the closure, whatever context the runner passes).
func (t *asker) toolHandler(ctx context.Context) monomind.ToolHandler {
	if t.tools == nil {
		return nil
	}
	log, _ := ctx.Value(toolCallKey{}).(*toolCallLog)
	return func(c context.Context, name string, args json.RawMessage) (string, error) {
		return t.tools.handler(c, log, name, args)
	}
}
