package hilsend

import (
	"strings"
	"sync/atomic"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Classified is a send a Classifier found: what it does, where its parts
// live in the node's config, the app it goes through, and the params the
// node resolves per input item.
type Classified struct {
	Action            string
	Channel           string
	To, Text, Subject []string
	PerItem           []string
}

// Classifier classifies node types this package's table doesn't know: the
// API packages' annotated ops (internal/apipkg SendOf, installed by
// noderegistry). Without one, those node types send as before.
type Classifier interface {
	// Classify reports whether running nodeType with config sends.
	Classify(nodeType string, config map[string]interface{}) (Classified, bool)
	// SendsSome reports whether nodeType sends for some ops.
	SendsSome(nodeType string) bool
}

type classifierBox struct{ c Classifier }

var installed atomic.Value // classifierBox

// SetClassifier installs c for the process; nil removes it.
func SetClassifier(c Classifier) { installed.Store(classifierBox{c}) }

func classifierOf() Classifier {
	b, _ := installed.Load().(classifierBox)
	return b.c
}

// channelOf is the channel the card shows: a Classifier's app, else the
// node type's.
func (t target) channelOf(nodeType string) string {
	if t.channel != "" {
		return t.channel
	}
	return channelOf(nodeType)
}

// accountOf is the sending account: on a Classifier's app, the account
// param of the node as for the node types this package knows.
func (t target) accountOf(nodeType string, config map[string]interface{}) Account {
	a := accountOf(nodeType, config)
	if t.channel != "" {
		a.Platform = t.channel
	}
	return a
}

// splitPerItem makes one send per input item of a node that resolves its
// params per item (an API package's app node): each item's config has
// those params resolved against it, and performs only that item, so the
// card shows the recipient and text that item would send and the user
// judges each one. Without input items the node sends once, as configured.
func splitPerItem(t target, config map[string]interface{}, input workflow.NodeInput) []one {
	if len(input.Items) == 0 {
		return split(t, config, nil)
	}
	engine := workflow.NewExpressionEngine()
	out := make([]one, 0, len(input.Items))
	for _, it := range input.Items {
		cfg := cloneMap(config)
		held := map[string]interface{}{}
		for _, k := range t.perItem {
			if v, ok := config[k]; ok {
				held[k] = v
			}
		}
		if resolved, err := engine.ResolveConfig(held, workflow.ExpressionContext{
			JSON: it.JSON, Node: input.NodeOutputs, WorkflowID: input.WorkflowID, ExecutionID: input.ExecutionID,
		}); err == nil {
			for k, v := range resolved {
				cfg[k] = v
			}
		}
		_, to := firstKey(cfg, append(append([]string{}, t.to...), toKeys...))
		to = strings.TrimSpace(to)
		out = append(out, one{recipient: Recipient{Name: to, ProfileURL: profileLike(to)}, config: cfg, input: []map[string]interface{}{it.JSON}})
	}
	return out
}

// valueAt is m's value at a send's param name: a top-level param, or for
// "a.b" option b of collection param a (an API package's annotated op,
// "updateFields.publicReply").
func valueAt(m map[string]interface{}, key string) interface{} {
	if v, ok := m[key]; ok || !strings.Contains(key, ".") {
		return v
	}
	head, rest, _ := strings.Cut(key, ".")
	sub, _ := m[head].(map[string]interface{})
	return valueAt(sub, rest)
}

// setAt sets m's value at key as valueAt reads it, copying the collection it
// writes into.
func setAt(m map[string]interface{}, key string, v interface{}) {
	head, rest, dotted := strings.Cut(key, ".")
	if _, ok := m[key]; ok || !dotted {
		m[key] = v
		return
	}
	sub, _ := m[head].(map[string]interface{})
	sub = cloneMap(sub)
	setAt(sub, rest, v)
	m[head] = sub
}
