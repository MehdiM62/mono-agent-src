package apipkg

import (
	"bytes"
	"encoding/json"
)

// NodeTypeOf is the node type a package registers ("" for extends).
func (p *Package) NodeTypeOf() string {
	if p.Extends != "" {
		return ""
	}
	if p.NodeType != "" {
		return p.NodeType
	}
	return NodeTypePrefix + p.ID
}

// TargetType is the node type whose config runs this package: its own node
// type, or the native node it extends.
func (p *Package) TargetType() string {
	if p.Extends != "" {
		return p.Extends
	}
	return p.NodeTypeOf()
}

// Op returns the operation for (resource, operation).
func (p *Package) Op(resource, operation string) (*Operation, bool) {
	for i := range p.Operations {
		o := &p.Operations[i]
		if o.Resource == resource && o.Operation == operation {
			return o, true
		}
	}
	return nil, false
}

// DefaultResource is the resource used when a config names none: the first
// resource ("" for a package without resources).
func (p *Package) DefaultResource() string {
	if len(p.Resources) == 0 {
		return ""
	}
	return p.Resources[0].Value
}

// DefaultOperation is the first operation listed for resource.
func (p *Package) DefaultOperation(resource string) string {
	for _, o := range p.Operations {
		if o.Resource == resource {
			return o.Operation
		}
	}
	return ""
}

// PrimaryPlatform is the connection platform of the first credential, or "".
func (p *Package) PrimaryPlatform() string {
	for _, c := range p.Credentials {
		if pl := c.EffectivePlatform(); pl != "" {
			return pl
		}
	}
	return ""
}

// EffectivePlatform is reuse_platform when set, else platform.
func (c *Credential) EffectivePlatform() string {
	if c.ReusePlatform != "" {
		return c.ReusePlatform
	}
	return c.Platform
}

// hasMethod reports whether the credential offers method m.
func (c *Credential) hasMethod(m string) bool {
	for _, x := range c.Methods {
		if x == m {
			return true
		}
	}
	return false
}

// Steps is one Step or a list of them in JSON.
type Steps []Step

// UnmarshalJSON accepts a single step object or a list of steps.
func (s *Steps) UnmarshalJSON(b []byte) error {
	t := bytes.TrimSpace(b)
	if len(t) > 0 && t[0] == '{' {
		var one Step
		if err := json.Unmarshal(t, &one); err != nil {
			return err
		}
		*s = Steps{one}
		return nil
	}
	var many []Step
	if err := json.Unmarshal(t, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// Poll is a request repeated until a condition holds.
type Poll struct {
	Step
	Until      string `json:"until"`
	IntervalMs int    `json:"interval_ms,omitempty"` // default 1000
	MaxWaitMs  int    `json:"max_wait_ms,omitempty"` // default 300000
	// MaxWaitSeconds (a template) overrides MaxWaitMs when it renders a
	// number: the wait comes from a param (n8n's timeout fields).
	MaxWaitSeconds string `json:"max_wait_seconds,omitempty"`
}

// Repeat is a step's wait condition: the step is resent every IntervalMs
// (default 1000) with res = the latest response until Until renders true,
// failing after the max wait (default 5 minutes).
type Repeat struct {
	Until          string `json:"until"`
	IntervalMs     int    `json:"interval_ms,omitempty"`
	MaxWaitMs      int    `json:"max_wait_ms,omitempty"`
	MaxWaitSeconds string `json:"max_wait_seconds,omitempty"`
}

// Stream reads a text/event-stream response event by event. Each event's
// data (JSON-decoded when it parses) is res for Until; the first event for
// which Until renders true becomes the response. The request fails when
// the stream ends first or after the timeout (default 5 minutes).
type Stream struct {
	Until          string `json:"until"`
	TimeoutMs      int    `json:"timeout_ms,omitempty"`
	TimeoutSeconds string `json:"timeout_seconds,omitempty"`
}

// HMACInject is a per-request HMAC signature header.
type HMACInject struct {
	Header   string `json:"header"`
	Secret   string `json:"secret"`
	Message  string `json:"message"`
	Alg      string `json:"alg,omitempty"`      // SHA256 (default), SHA1, SHA512
	Encoding string `json:"encoding,omitempty"` // base64 (default) or hex
	Prefix   string `json:"prefix,omitempty"`
}

// AzureSharedKeyInject: Account and Key (base64) are templates.
type AzureSharedKeyInject struct {
	Account string `json:"account"`
	Key     string `json:"key"`
}

// AzureCosmosInject: Key (base64 master key) is a template.
type AzureCosmosInject struct {
	Key string `json:"key"`
}
