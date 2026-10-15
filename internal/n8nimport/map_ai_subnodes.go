package n8nimport

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n's LangChain sub-nodes (chat models, memories, output parsers,
// embeddings, document loaders, text splitters) are kept as mono AI
// sub-nodes: nodes of their own attached to the node that uses them
// through a connection whose source and target handle are n8n's connection
// type ("ai_languageModel", "ai_embedding", …). The engine never runs them
// as steps; the root reads their config (workflow.NodeInput.SubNodes).

// attachedSub is a sub-node a mapping keeps. target is the config map of
// the node it attaches to: a node of the mapping (its Config or a chain
// step's), or another kept sub-node's config.
type attachedSub struct {
	name   string // the n8n sub-node's name
	kind   string
	typ    string
	config map[string]interface{}
	target map[string]interface{}
}

// attach keeps n8n sub-node name as a mono sub-node of type typ attached to
// the node whose config is target.
func (c *MapContext) attach(name, kind, typ string, config, target map[string]interface{}) {
	c.attached = append(c.attached, attachedSub{name: name, kind: kind, typ: typ, config: config, target: target})
}

// configKey identifies a config map by identity, not content.
func configKey(m map[string]interface{}) uintptr { return reflect.ValueOf(m).Pointer() }

// placedSub is a mono sub-node written for an n8n sub-node.
type placedSub struct {
	id, typ string
	config  map[string]interface{}
}

// attachedReport records where an n8n sub-node landed, for its report.
type attachedReport struct {
	name, parent, kind, typ, id string
}

// placeSubNodes writes the sub-nodes ctx kept and connects each one to its
// target among nodes (config identity → mono id of the mapping's nodes) or
// the sub-nodes placed before it. One whose target the mapping dropped is left out (its n8n
// node stays reported as folded). An n8n sub-node feeding several roots
// becomes one mono node when its config is the same for all of them.
func (b *builder) placeSubNodes(ctx *MapContext, nodes map[uintptr]string) {
	names := map[uintptr]string{}
	for _, a := range ctx.attached {
		target, ok := nodes[configKey(a.target)]
		if !ok {
			continue
		}
		parent := ctx.NodeName
		if n, ok := names[configKey(a.target)]; ok {
			parent = n
		}
		id := ""
		for _, p := range b.subPlaced[a.name] {
			if p.typ == a.typ && reflect.DeepEqual(p.config, a.config) {
				id = p.id
			}
		}
		if id == "" {
			id = b.newSubNode(a)
		}
		nodes[configKey(a.config)], names[configKey(a.config)] = id, a.name
		b.connectSub(id, a.kind, target)
		b.attachedReports = append(b.attachedReports, attachedReport{name: a.name, parent: parent, kind: a.kind, typ: a.typ, id: id})
	}
}

// newSubNode writes the mono node of a kept sub-node at the n8n node's
// position.
func (b *builder) newSubNode(a attachedSub) string {
	name, idWant := a.name, a.name
	var x, y float64
	disabled := false
	if n := b.byName[a.name]; n != nil {
		x, y = n.position()
		disabled = n.Disabled
		if n.ID != "" {
			idWant = string(n.ID)
		}
	}
	if k := len(b.subPlaced[a.name]); k > 0 {
		name = fmt.Sprintf("%s (%d)", a.name, k+1)
		idWant = name
	}
	id := b.newID(idWant)
	fn := workflow.WorkflowFileNode{ID: id, Type: a.typ, Name: name, Disabled: disabled, Config: a.config}
	fn.Position.X, fn.Position.Y = x, y
	b.out.Nodes = append(b.out.Nodes, fn)
	if b.subPlaced == nil {
		b.subPlaced = map[string][]placedSub{}
	}
	b.subPlaced[a.name] = append(b.subPlaced[a.name], placedSub{id: id, typ: a.typ, config: a.config})
	return id
}

// connectSub attaches sub-node src to dst through a kind connection.
func (b *builder) connectSub(src, kind, dst string) {
	for _, c := range b.out.Connections {
		if c.Source == src && c.SourceHandle == kind && c.Target == dst {
			return
		}
	}
	b.nextConn++
	b.out.Connections = append(b.out.Connections, workflow.WorkflowFileEdge{
		ID: fmt.Sprintf("c%d", b.nextConn), Source: src, SourceHandle: kind, Target: dst, TargetHandle: kind,
	})
}

const foldedPrefix = "sub-node folded into "

// reportAttached turns the reports of the kept sub-nodes from folded into
// mapped, naming the node each one is attached to.
func (b *builder) reportAttached() {
	for _, r := range b.attachedReports {
		idx, ok := b.reports[r.name]
		if !ok {
			continue
		}
		nr := &b.rep.Nodes[idx]
		if nr.Status == StatusFolded {
			nr.Status = ""
			kept := nr.Issues[:0]
			for _, is := range nr.Issues {
				if !strings.HasPrefix(is.Message, foldedPrefix) {
					kept = append(kept, is)
				}
			}
			nr.Issues = kept
		}
		switch {
		case nr.MonoType == "":
			nr.MonoType, nr.MonoID = r.typ, r.id
		case !strings.Contains(nr.MonoType, r.typ):
			nr.MonoType += ", " + r.typ
		}
		nr.Issues = append(nr.Issues, Issue{Severity: SevInfo, Message: fmt.Sprintf("sub-node of %q (%s connection)", r.parent, r.kind)})
	}
}
