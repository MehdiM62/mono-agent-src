package n8nimport_test

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	_ "github.com/monoes/mono-agent/internal/noderegistry"
)

// opsFixture is testdata/n8n_ops.json: every non-trigger node type of
// n8n's nodes-base and nodes-langchain packages with the values of its
// resource and operation parameters in its latest version (Param names the operation parameter
// when it is not "operation"; Sub is the connection kind of a LangChain
// sub-node, which only exists attached to a parent). An operation listed
// under the resource "" is selected without a resource. "<type>Tool" entries
// with Sub ai_tool are the tool variants n8n generates for every node
// marked usableAsTool, with their base node's operations; they are measured
// attached to an agent and on their own in the main flow.
type opsFixture struct {
	Source string `json:"source"`
	Nodes  []struct {
		Type    string              `json:"type"`
		Version float64             `json:"version"`
		Param   string              `json:"param"`
		Sub     string              `json:"sub"`
		Ops     map[string][]string `json:"ops"`
		// When gives the parameters that show an operation outside the
		// resource selector (Discord's webhook connection).
		When map[string]map[string]interface{} `json:"when"`
	} `json:"nodes"`
}

// allowEntry is one known gap in testdata/n8n_ops_allow.json. Key is the
// node type, optionally followed by "#resource.operation" (or
// "#operation" for nodes without resources); a bare type covers all of
// the type's operations.
type allowEntry struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

var allowCategories = map[string]bool{"engine": true, "impossible": true, "agents": true, "ours-todo": true}

// subParents is the chain of parent nodes a sub-node of each connection
// kind attaches to, innermost first, with the parent's parameters.
var subParents = map[string]parentChain{
	"ai_languageModel": {{lc + "agent", "", nil}},
	"ai_tool":          {{lc + "agent", "", nil}},
	"ai_memory":        {{lc + "agent", "", nil}},
	"ai_outputParser":  {{lc + "chainLlm", "", nil}},
	"ai_retriever":     {{lc + "chainRetrievalQa", "", nil}},
	"ai_embedding":     {{lc + "vectorStoreInMemory", "", map[string]interface{}{"mode": "insert", "memoryKey": "k"}}},
	"ai_document":      {{lc + "vectorStoreInMemory", "", map[string]interface{}{"mode": "insert", "memoryKey": "k"}}},
	"ai_reranker":      {{lc + "vectorStoreInMemory", "", map[string]interface{}{"mode": "load", "memoryKey": "k", "prompt": "q"}}},
	"ai_textSplitter": {{lc + "documentDefaultDataLoader", "ai_document", nil},
		{lc + "vectorStoreInMemory", "", map[string]interface{}{"mode": "insert", "memoryKey": "k"}}},
	"ai_vectorStore": {{lc + "toolVectorStore", "ai_tool", nil}, {lc + "agent", "", nil}},
}

const lc = "@n8n/n8n-nodes-langchain."

// extraParityCases are parameter variants outside resource/operation that
// decide whether a node imports (the Code node's language).
var extraParityCases = []parityCase{
	{typ: "n8n-nodes-base.code", key: "n8n-nodes-base.code#language=python", version: 1,
		when: map[string]interface{}{"language": "python"}},
	{typ: "n8n-nodes-base.code", key: "n8n-nodes-base.code#language=pythonNative", version: 2,
		when: map[string]interface{}{"language": "pythonNative"}},
}

type parityCase struct {
	typ, res, op string
	key          string
	version      float64
	when         map[string]interface{}
	// label tells apart extra measurements of one key (" (standalone)",
	// " (under chainLlm)") in the results.
	label string
}

// parentChain is the chain of parent nodes a sub-node attaches to,
// innermost first, with the parent's parameters.
type parentChain []struct {
	typ, kind string
	params    map[string]interface{}
}

// extraSubParents are more parents a sub-node of a kind is measured under,
// by label: chat models also run LLM chains.
var extraSubParents = map[string]map[string]parentChain{
	"ai_languageModel": {"chainLlm": {{lc + "chainLlm", "", nil}}},
}

// subChildren are sub-nodes a node needs to map at all (a vector store
// tool searches the store attached to it).
var subChildren = map[string]struct {
	typ, kind string
	params    map[string]interface{}
}{
	lc + "toolVectorStore": {lc + "vectorStoreInMemoryLoad", "ai_vectorStore", map[string]interface{}{"memoryKey": "k"}},
}

func (pc parityCase) String() string {
	if pc.op == "" {
		return pc.typ
	}
	return pc.key
}

// parityStatus converts one case and says what a user gets.
func parityStatus(t *testing.T, sub, param string, pc parityCase) (string, string) {
	return parityStatusUnder(t, sub, subParents[sub], param, pc)
}

// parityStatusUnder converts one case with the sub-node attached to the
// given parents. A sub-node that is placed on its own (an agent's tool)
// counts as unmapped when it, or a parent, becomes a placeholder.
func parityStatusUnder(t *testing.T, sub string, parents parentChain, param string, pc parityCase) (string, string) {
	t.Helper()
	params := map[string]interface{}{}
	if pc.res != "" {
		params["resource"] = pc.res
	}
	if pc.op != "" {
		params[param] = pc.op
	}
	for k, v := range pc.when {
		params[k] = v
	}
	nodes := []interface{}{map[string]interface{}{"id": "n0", "name": "Node", "type": pc.typ, "typeVersion": pc.version,
		"position": []int{0, 0}, "parameters": params}}
	conns := map[string]interface{}{}
	root := "Node"
	if ch, ok := subChildren[pc.typ]; ok {
		nodes = append(nodes, map[string]interface{}{"id": "c0", "name": "Child", "type": ch.typ, "typeVersion": 1,
			"position": []int{0, -200}, "parameters": ch.params})
		conns["Child"] = map[string]interface{}{ch.kind: []interface{}{[]interface{}{
			map[string]interface{}{"node": "Node", "type": ch.kind, "index": 0}}}}
	}
	if sub != "" {
		child, kind := "Node", sub
		for i, p := range parents {
			name := fmt.Sprintf("Parent%d", i)
			pp := p.params
			if pp == nil {
				pp = map[string]interface{}{}
			}
			nodes = append(nodes, map[string]interface{}{"id": fmt.Sprintf("p%d", i), "name": name, "type": p.typ,
				"typeVersion": 1, "position": []int{0, 200 * (i + 1)}, "parameters": pp})
			conns[child] = map[string]interface{}{kind: []interface{}{[]interface{}{
				map[string]interface{}{"node": name, "type": kind, "index": 0}}}}
			child, kind, root = name, p.kind, name
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"name": "parity", "nodes": nodes, "connections": conns})
	_, rep, err := n8nimport.Convert(raw)
	if err != nil {
		t.Fatalf("%s: convert: %v", pc, err)
	}
	for _, nr := range rep.Nodes {
		if nr.Name != root && nr.Name != "Child" && nr.Status == n8nimport.StatusUnmapped {
			var msgs []string
			for _, is := range nr.Issues {
				msgs = append(msgs, is.Message)
			}
			return "unmapped", nr.Name + ": " + strings.Join(msgs, "; ")
		}
	}
	for _, nr := range rep.Nodes {
		if nr.Name != root {
			continue
		}
		var msgs []string
		mentioned := false
		for _, is := range nr.Issues {
			msgs = append(msgs, is.Message)
			if sub != "" && is.Severity != n8nimport.SevInfo && (is.Value == "Node" || strings.Contains(is.Message, shortN8nType(pc.typ))) {
				mentioned = true
			}
		}
		switch {
		case nr.Status == n8nimport.StatusUnmapped:
			return "unmapped", strings.Join(msgs, "; ")
		case sub == "" && nr.Status == n8nimport.StatusPartial, mentioned:
			return "partial", strings.Join(msgs, "; ")
		}
		return "mapped", ""
	}
	t.Fatalf("%s: no report for %s", pc, root)
	return "", ""
}

func shortN8nType(t string) string { return t[strings.LastIndex(t, ".")+1:] }

// TestN8nOperationParity converts every n8n node type and operation (one
// node per workflow, or a sub-node under its usual parent) and fails for
// each placeholder that testdata/n8n_ops_allow.json does not list. Allow
// entries that no longer match a gap fail too, so the list stays exact.
// N8N_PARITY_OUT=<file> writes the per-case results as JSON.
func TestN8nOperationParity(t *testing.T) {
	var fx opsFixture
	readJSON(t, "testdata/n8n_ops.json", &fx)
	if len(fx.Nodes) < 400 {
		t.Fatalf("only %d node types in the fixture", len(fx.Nodes))
	}
	var allow []allowEntry
	readJSON(t, "testdata/n8n_ops_allow.json", &allow)
	allowed := map[string]allowEntry{}
	for _, a := range allow {
		if !allowCategories[a.Category] || strings.TrimSpace(a.Reason) == "" {
			t.Errorf("allow entry %s: category %q / reason %q", a.Key, a.Category, a.Reason)
		}
		if _, dup := allowed[a.Key]; dup {
			t.Errorf("allow entry %s is listed twice", a.Key)
		}
		allowed[a.Key] = a
	}
	type result struct {
		Key      string `json:"key"`
		Status   string `json:"status"`
		Category string `json:"category,omitempty"`
		Detail   string `json:"detail,omitempty"`
	}
	var results []result
	counts := map[string]int{}
	used := map[string]bool{}
	// record converts one case; a placeholder must be allowed by its own
	// key or, for a bare-type entry, by its node type.
	// A "<type>Tool" variant has the gaps of its base type: its allow
	// entries are the base type's.
	fixtureTypes := map[string]bool{}
	for _, n := range fx.Nodes {
		fixtureTypes[n.Type] = true
	}
	allowKeys := func(pc parityCase) []string {
		keys := []string{pc.key, pc.typ}
		if base := strings.TrimSuffix(pc.typ, "Tool"); base != pc.typ && fixtureTypes[base] {
			keys = append(keys, base+strings.TrimPrefix(pc.key, pc.typ), base)
		}
		return keys
	}
	record := func(pc parityCase, sub string, parents parentChain, param string) {
		status, detail := parityStatusUnder(t, sub, parents, param, pc)
		counts[status]++
		r := result{Key: pc.key + pc.label, Status: status}
		if status == "unmapped" {
			var a allowEntry
			ok := false
			for _, key := range allowKeys(pc) {
				if a, ok = allowed[key]; ok {
					used[key] = true
					break
				}
			}
			if !ok {
				t.Errorf("%s%s imports as a placeholder and is not in n8n_ops_allow.json: %s", pc.key, pc.label, detail)
			}
			r.Category, r.Detail = a.Category, detail
		}
		results = append(results, r)
	}
	for _, n := range fx.Nodes {
		param := n.Param
		if param == "" {
			param = "operation"
		}
		var cases []parityCase
		for _, res := range sortedOpKeys(n.Ops) {
			for _, op := range n.Ops[res] {
				key := n.Type + "#" + op
				if res != "" {
					key = n.Type + "#" + res + "." + op
				}
				cases = append(cases, parityCase{typ: n.Type, res: res, op: op, key: key, version: n.Version, when: n.When[op]})
			}
		}
		if len(cases) == 0 {
			cases = []parityCase{{typ: n.Type, key: n.Type, version: n.Version}}
		}
		for _, pc := range cases {
			record(pc, n.Sub, subParents[n.Sub], param)
			for _, label := range sortedOpKeys(extraSubParents[n.Sub]) {
				pc.label = " (under " + label + ")"
				record(pc, n.Sub, extraSubParents[n.Sub][label], param)
			}
			// n8n places a tool variant in the main flow too, where it
			// runs like its base node.
			if n.Sub == "ai_tool" && fixtureTypes[strings.TrimSuffix(n.Type, "Tool")] && strings.HasSuffix(n.Type, "Tool") {
				pc.label = " (standalone)"
				record(pc, "", nil, param)
			}
		}
	}
	for _, pc := range extraParityCases {
		record(pc, "", nil, "operation")
	}
	for _, a := range allow {
		if !used[a.Key] {
			t.Errorf("allow entry %s matches no placeholder any more; remove it", a.Key)
		}
	}
	t.Logf("%s: %d node types, %d cases: %d mapped, %d partial, %d unmapped",
		fx.Source, len(fx.Nodes), len(results), counts["mapped"], counts["partial"], counts["unmapped"])
	if out := os.Getenv("N8N_PARITY_OUT"); out != "" {
		raw, _ := json.MarshalIndent(results, "", " ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func sortedOpKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
