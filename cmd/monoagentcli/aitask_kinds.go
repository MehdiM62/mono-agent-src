package main

import (
	"sync"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/noderegistry"
)

// The workflow-generate kind refuses a result whose workflow uses a node type
// that does not exist. The registry imports the ai.task node, so internal/aitask
// can't import it: it is handed over here, built once on first use.
var aitaskNodeTypes = sync.OnceValue(func() interface{ Has(string) bool } { return noderegistry.Build(nil) })

func init() {
	aitask.WorkflowNodeTypeKnown = func(t string) bool { return aitaskNodeTypes().Has(t) }
}
