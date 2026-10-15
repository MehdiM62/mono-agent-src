// Package generate turns a natural-language description into a validated,
// inactive draft workflow. The model (any monomind runtime) is asked for one
// workflow JSON document; the document is parsed and validated, and the
// validation errors go back to the model until it passes or the attempts
// run out. Callers (the `workflow generate` command and the MCP tool) supply
// the turn, the validator and the node catalog, so the loop itself needs no
// monomind, database or registry and is tested with fakes.
package generate

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/monoes/mono-agent/internal/workflow"
)

// NodeInfo is one node type in the catalog the prompt lists.
type NodeInfo struct {
	Type  string
	Title string
	// Outputs describes what the node emits, when known (ref node docs).
	Outputs string
	// Required are the config fields the node's schema marks required.
	Required []Field
	// Optional are the other config keys of the schema.
	Optional []string
}

// Field is one required config field.
type Field struct {
	Key     string
	Type    string
	Options []string
}

// maxOptions caps the enumerated values listed for one field.
const maxOptions = 8

// maxTitle and maxOutputs cap the title and outputs text of one node.
const (
	maxTitle   = 90
	maxOutputs = 100
)

// maxOptional caps the optional keys listed for one node.
const maxOptional = 6

// BuildCatalog makes the catalog for types (registry types plus trigger
// types) from their form schemas. Deprecated types are left out, so the
// model is never offered one validation would reject. title and outputs may
// be nil; a missing title is derived from the type.
func BuildCatalog(types []string, title, outputs func(string) string) []NodeInfo {
	seen := map[string]bool{}
	var out []NodeInfo
	for _, t := range types {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		if _, deprecated := workflow.IsDeprecatedNodeType(t); deprecated {
			continue
		}
		info := NodeInfo{Type: t}
		if title != nil {
			info.Title = title(t)
		}
		if info.Title == "" {
			info.Title = DeriveTitle(t)
		}
		if outputs != nil {
			info.Outputs = outputs(t)
		}
		if s, err := workflow.LoadDefaultSchema(t); err == nil && s != nil {
			for _, f := range s.Fields {
				if f.Required {
					info.Required = append(info.Required, Field{Key: f.Key, Type: f.Type, Options: f.Options})
				} else {
					info.Optional = append(info.Optional, f.Key)
				}
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// TriggerTypes are the trigger node types: they have schemas but no
// registry executor, so a registry's Types() never lists them.
func TriggerTypes() []string {
	var out []string
	for _, t := range workflow.ListEmbeddedSchemas() {
		if strings.HasPrefix(t, "trigger.") {
			out = append(out, t)
		}
	}
	return out
}

// DeriveTitle makes a title from a node type ("service.google_sheets" →
// "Google Sheets").
func DeriveTitle(t string) string {
	name := t
	if i := strings.Index(t, "."); i >= 0 {
		name = t[i+1:]
	}
	words := strings.Split(name, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// catalogText renders the catalog one line per node:
//
//	type — Title. req: key(type: a|b), key2(text); opt: k1, k2; out: …
func catalogText(catalog []NodeInfo) string {
	var b strings.Builder
	for _, n := range catalog {
		fmt.Fprintf(&b, "%s — %s", n.Type, clip(n.Title, maxTitle))
		if len(n.Required) > 0 {
			parts := make([]string, 0, len(n.Required))
			for _, f := range n.Required {
				p := f.Key
				if f.Type != "" {
					p += "(" + f.Type
					if len(f.Options) > 0 {
						opts := f.Options
						more := ""
						if len(opts) > maxOptions {
							opts, more = opts[:maxOptions], "|…"
						}
						p += ": " + strings.Join(opts, "|") + more
					}
					p += ")"
				}
				parts = append(parts, p)
			}
			b.WriteString(". req: " + strings.Join(parts, ", "))
		}
		if len(n.Optional) > 0 {
			opt := n.Optional
			more := ""
			if len(opt) > maxOptional {
				more = fmt.Sprintf(", +%d more", len(opt)-maxOptional)
				opt = opt[:maxOptional]
			}
			b.WriteString("; opt: " + strings.Join(opt, ", ") + more)
		}
		if n.Outputs != "" {
			b.WriteString("; out: " + clip(n.Outputs, maxOutputs))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// clip folds s onto one line and cuts it to at most max bytes (at a rune
// boundary), marking a cut with "…".
func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ,;") + "…"
}

// systemPromptHead is the authoring guidance. It carries the in-app chat
// builder's rules (canvasSystemPrompt in cmd/monoagentcli/chat.go: "main"
// handles, 250px spacing) and the workflow JSON format of `ref workflow`,
// reshaped for a turn that answers with a document instead of tool calls.
const systemPromptHead = `You are the workflow builder of Mono Agent, a local workflow automation engine (an n8n alternative).
You turn the user's description into ONE workflow, written as ONE JSON document. You have no tools: never describe what you would do, never ask questions, never add prose. Answer with the JSON document only (a single ` + "```json" + ` fence is fine).

WORKFLOW JSON FORMAT
{
  "name": "Short descriptive name",
  "description": "One sentence: what it does",
  "is_active": false,
  "nodes": [
    { "id": "n1", "type": "trigger.manual", "name": "Start",
      "position": {"x": 100, "y": 100}, "config": {} },
    { "id": "n2", "type": "http.request", "name": "Fetch",
      "position": {"x": 350, "y": 100},
      "config": {"method": "GET", "url": "https://example.com/api"} }
  ],
  "connections": [
    { "id": "c1", "source": "n1", "source_handle": "main",
      "target": "n2", "target_handle": "main" }
  ]
}

RULES
1. Use ONLY node types from the NODE CATALOG below, spelled exactly. Never invent a type.
2. Exactly one trigger.* node starts the workflow. Use trigger.manual unless the description asks for a schedule (trigger.schedule, "cron" is 6-field: sec min hour day month weekday, e.g. "0 0 9 * * *") or a webhook (trigger.webhook, needs "path").
3. Node ids are unique short strings (n1, n2, ...). Every connection's source and target is an existing node id. No cycles.
4. Use "main" as source_handle and target_handle, except for nodes with named outputs: core.if "true"/"false"; core.filter "main"/"rejected"; core.switch one handle per case value plus "default"; core.split_in_batches "batch"/"done"; core.compare_datasets "added"/"removed"/"changed"/"unchanged".
5. Space nodes: increment position.x by 250 per step; put parallel branches on different position.y rows. Rejoin branches with core.merge.
6. Fill every required config field ("req:" in the catalog). Use real values from the description; for a value the user must supply later (an account, a recipient, an API resource id) use an obvious placeholder such as "REPLACE_ME".
7. Pass data between nodes with expressions: {{ $json.field }} is the current item's field, {{ $node["Node Name"].json.field }} a previous node's first item.
8. Never write a real password, token or API key into config: leave "REPLACE_ME" in password fields and prefer a saved connection (credential_id) where the node takes one.
9. agent.ask runs a local AI agent: its "runtime" field is the agent CLI id (claude, codex, ...). Use it for free-form AI steps (summaries, drafting, extraction).
10. "is_active" stays false: the workflow is saved as an inactive draft for the user to review.

NODE CATALOG (type — title. req: required config fields; opt: some optional fields; out: what it emits)
`

// SystemPrompt is the full system prompt for a catalog.
func SystemPrompt(catalog []NodeInfo) string {
	return systemPromptHead + catalogText(catalog)
}

// maxEcho caps how much of a failed answer is sent back in a retry prompt.
const maxEcho = 24 << 10

// userPrompt is the turn's prompt: the description, and on a retry the
// previous answer with what was wrong with it.
func userPrompt(description, previous string, problems []string) string {
	var b strings.Builder
	b.WriteString("Build a workflow for this description:\n\n")
	b.WriteString(strings.TrimSpace(description))
	b.WriteString("\n")
	if len(problems) == 0 {
		b.WriteString("\nAnswer with the workflow JSON document only.")
		return b.String()
	}
	if previous != "" {
		if len(previous) > maxEcho {
			previous = previous[:maxEcho] + "\n…(truncated)"
		}
		b.WriteString("\nYour previous answer was:\n")
		b.WriteString(previous)
		b.WriteString("\n")
	}
	b.WriteString("\nIt was rejected:\n")
	for _, p := range problems {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("\nFix every problem and answer with the complete corrected workflow JSON document only.")
	return b.String()
}
