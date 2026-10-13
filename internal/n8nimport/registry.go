package n8nimport

import (
	"sort"
	"sync"
)

// Mapping is what a Mapper returns for one n8n node.
type Mapping struct {
	// Type is the mono node type, e.g. "comm.slack".
	Type string
	// Config is the mono node config. Expression strings must already be
	// translated (use MapContext.Str / Value).
	Config map[string]interface{}
	// Outputs names the mono output handle for each n8n "main" output
	// index, on the node that produces the outputs (the last of a chain).
	// Nil means a single output on "main".
	Outputs []string
	// Chain lists nodes that run after this one (the n8n node becomes a
	// small chain, e.g. a schedule trigger followed by an IMAP read).
	// Incoming connections go to the first node, outgoing ones leave the
	// last node.
	Chain []ChainStep
	// Alternates are extra trigger nodes that start the same downstream
	// path (an n8n schedule with several rules becomes one trigger per rule).
	Alternates []ChainStep
	// EntrySuffix names the first node of a chain (the n8n name plus this
	// suffix); the last node keeps the n8n name.
	EntrySuffix string
	// TagInputs inserts a core.set before every incoming connection that
	// records the n8n input index in the item field "__n8n_input", for
	// nodes whose behaviour depends on which input an item came from.
	TagInputs bool
	// Unmapped marks a node with no mono equivalent. The converter writes
	// a core.code stub that fails with a clear message.
	Unmapped bool
}

// ChainStep is one extra node of a Mapping chain.
type ChainStep struct {
	Type   string
	Suffix string // appended to the n8n name for steps before the last
	Config map[string]interface{}
}

// Mapper converts one n8n node (parameters, typeVersion, credentials, all
// reachable through the MapContext) into a mono node. Issues and notes
// for the report are recorded on the context.
type Mapper func(c *MapContext) Mapping

// AppMapper maps the n8n node types of a declarative API package. Package
// node configs use n8n parameter names verbatim (resource, operation and
// the operation's fields), so the mapping is near-identity: the type
// becomes the package node type, parameters are copied with expressions
// translated, and the n8n credential type picks the connection platform.
type AppMapper struct {
	// N8nTypes are the n8n node types the package imports,
	// e.g. "n8n-nodes-base.trello".
	N8nTypes []string
	// NodeType is the package's mono node type ("app.trello"). Empty when
	// Extends is set.
	NodeType string
	// Extends is the native mono node the package adds operations to
	// ("service.github"); used as the node type when NodeType is empty.
	Extends string
	// CredentialPlatform maps an n8n credential type ("trelloApi") to the
	// mono connection platform ("trello").
	CredentialPlatform map[string]string
	// DefaultResource and DefaultOperation are filled in when the n8n
	// export omits them (n8n leaves out parameters equal to their default).
	DefaultResource  string
	DefaultOperation string
	// ParamMap renames n8n parameters per operation, keyed
	// "resource.operation" (or "operation" when there is no resource):
	// n8n parameter name → package field name. Only for names that differ.
	ParamMap map[string]map[string]string
}

var (
	regMu   sync.RWMutex
	mappers = map[string]Mapper{}
	apps    = map[string]AppMapper{}
)

// Register installs the Mapper for an n8n node type (the full type string,
// e.g. "n8n-nodes-base.slack"). A later registration replaces an earlier one.
func Register(n8nType string, m Mapper) {
	regMu.Lock()
	defer regMu.Unlock()
	mappers[n8nType] = m
}

// RegisterApp installs an AppMapper for each of its n8n types. A native
// Mapper registered for the same type still wins; the app mapping is used
// for operations the native mapper reports as unsupported.
func RegisterApp(a AppMapper) {
	regMu.Lock()
	defer regMu.Unlock()
	for _, t := range a.N8nTypes {
		apps[t] = a
	}
}

func lookup(n8nType string) (Mapper, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	m, ok := mappers[n8nType]
	return m, ok
}

func lookupApp(n8nType string) (AppMapper, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := apps[n8nType]
	return a, ok
}

// SupportedTypes lists the n8n node types with a native Mapper and those
// handled by an AppMapper, both sorted.
func SupportedTypes() (native, app []string) {
	regMu.RLock()
	defer regMu.RUnlock()
	for t := range mappers {
		native = append(native, t)
	}
	for t := range apps {
		if _, ok := mappers[t]; !ok {
			app = append(app, t)
		}
	}
	sort.Strings(native)
	sort.Strings(app)
	return native, app
}

// mapApp is the generic AppMapper conversion.
func mapApp(a AppMapper, c *MapContext) Mapping {
	nodeType := a.NodeType
	if nodeType == "" {
		nodeType = a.Extends
	}
	cfg := map[string]interface{}{}
	resource := c.Literal("resource", a.DefaultResource)
	operation := c.Literal("operation", a.DefaultOperation)
	if resource != "" {
		cfg["resource"] = resource
	}
	if operation != "" {
		cfg["operation"] = operation
	}
	key := operation
	if resource != "" {
		key = resource + "." + operation
	}
	rename := a.ParamMap[key]
	for k := range c.Params {
		if k == "resource" || k == "operation" {
			continue
		}
		dst := k
		if r, ok := rename[k]; ok {
			dst = r
		}
		cfg[dst] = c.Value(k)
	}
	credTypes := make([]string, 0, len(a.CredentialPlatform))
	for t := range a.CredentialPlatform {
		credTypes = append(credTypes, t)
	}
	sort.Strings(credTypes)
	for _, t := range credTypes {
		if _, used := c.Credentials[t]; used {
			cfg["credential_id"] = c.Credential(a.CredentialPlatform[t], t)
			break
		}
	}
	if _, ok := cfg["credential_id"]; !ok && len(credTypes) > 0 {
		cfg["credential_id"] = c.Credential(a.CredentialPlatform[credTypes[0]])
	}
	return Mapping{Type: nodeType, Config: cfg}
}
