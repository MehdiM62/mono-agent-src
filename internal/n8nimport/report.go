package n8nimport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Severity of a report issue.
type Severity string

const (
	SevInfo    Severity = "info"    // approximation that keeps the meaning
	SevWarning Severity = "warning" // something not carried over; the node is partial
	SevError   Severity = "error"   // the node could not be mapped
)

// Status of one n8n node after conversion.
type Status string

const (
	StatusFull     Status = "full"     // mapped with everything carried over
	StatusPartial  Status = "partial"  // mapped, but some settings were not
	StatusUnmapped Status = "unmapped" // replaced by a failing core.code stub
	StatusFolded   Status = "folded"   // a sub-node merged into its parent (LangChain model, tool, …)
	StatusDropped  Status = "dropped"  // not a runtime node (sticky note)
)

// Issue is one report line about a node.
type Issue struct {
	Severity Severity    `json:"severity"`
	Param    string      `json:"param,omitempty"`
	Value    interface{} `json:"value,omitempty"`
	Message  string      `json:"message"`
}

// ExprIssue is an expression left untranslated in a node parameter.
type ExprIssue struct {
	Node       string `json:"node,omitempty"`
	Param      string `json:"param"`
	Expression string `json:"expression"`
	Reason     string `json:"reason"`
}

// NodeReport describes the conversion of one n8n node.
type NodeReport struct {
	Name        string      `json:"name"`
	N8nType     string      `json:"n8n_type"`
	TypeVersion float64     `json:"type_version"`
	MonoType    string      `json:"mono_type,omitempty"`
	MonoID      string      `json:"mono_id,omitempty"`
	Status      Status      `json:"status"`
	Issues      []Issue     `json:"issues,omitempty"`
	Expressions []ExprIssue `json:"expressions,omitempty"`
}

// CredentialReport is a connection the imported workflow needs.
type CredentialReport struct {
	Platform string   `json:"platform"`
	N8nType  string   `json:"n8n_credential_type,omitempty"`
	N8nName  string   `json:"n8n_credential_name,omitempty"`
	Nodes    []string `json:"nodes"`
	Hint     string   `json:"hint"`
}

// Counts summarises a Report.
type Counts struct {
	Nodes        int `json:"nodes"`
	Full         int `json:"full"`
	Partial      int `json:"partial"`
	Unmapped     int `json:"unmapped"`
	Folded       int `json:"folded"`
	Dropped      int `json:"dropped"`
	Expressions  int `json:"expressions_untranslated"`
	Credentials  int `json:"credentials"`
	MonoNodes    int `json:"mono_nodes"`
	Connections  int `json:"mono_connections"`
	RemovedLoops int `json:"loops_flattened"`
}

// Report is the outcome of converting one n8n workflow.
type Report struct {
	Workflow    string             `json:"workflow"`
	Complete    bool               `json:"complete"`
	Counts      Counts             `json:"counts"`
	Nodes       []NodeReport       `json:"nodes"`
	Credentials []CredentialReport `json:"credentials"`
	Expressions []ExprIssue        `json:"expressions"`
	Notes       []string           `json:"notes,omitempty"`
}

func (r *Report) note(format string, a ...interface{}) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, a...))
}

// finish computes counts and the per-node status from the issues.
func (r *Report) finish() {
	c := Counts{Nodes: len(r.Nodes), Credentials: len(r.Credentials), MonoNodes: r.Counts.MonoNodes,
		Connections: r.Counts.Connections, RemovedLoops: r.Counts.RemovedLoops}
	r.Expressions = nil
	for i := range r.Nodes {
		n := &r.Nodes[i]
		if n.Status == "" {
			n.Status = StatusFull
			for _, is := range n.Issues {
				if is.Severity == SevWarning {
					n.Status = StatusPartial
				}
			}
			if len(n.Expressions) > 0 {
				n.Status = StatusPartial
			}
		}
		switch n.Status {
		case StatusFull:
			c.Full++
		case StatusPartial:
			c.Partial++
		case StatusUnmapped:
			c.Unmapped++
		case StatusFolded:
			c.Folded++
		case StatusDropped:
			c.Dropped++
		}
		for _, e := range n.Expressions {
			e.Node = n.Name
			r.Expressions = append(r.Expressions, e)
		}
	}
	c.Expressions = len(r.Expressions)
	r.Counts = c
	r.Complete = c.Unmapped == 0
	if r.Credentials == nil {
		r.Credentials = []CredentialReport{}
	}
	if r.Expressions == nil {
		r.Expressions = []ExprIssue{}
	}
}

// addCredential merges a credential use into the report.
func (r *Report) addCredential(node string, u credUse) {
	for i := range r.Credentials {
		cr := &r.Credentials[i]
		if cr.Platform == u.Platform && cr.N8nName == u.N8nName {
			if !contains(cr.Nodes, node) {
				cr.Nodes = append(cr.Nodes, node)
			}
			return
		}
	}
	hint := u.Hint
	if hint == "" {
		hint = fmt.Sprintf("monoagentcli connect %s", u.Platform)
	}
	r.Credentials = append(r.Credentials, CredentialReport{Platform: u.Platform, N8nType: u.N8nType,
		N8nName: u.N8nName, Nodes: []string{node}, Hint: hint})
	sort.SliceStable(r.Credentials, func(i, j int) bool { return r.Credentials[i].Platform < r.Credentials[j].Platform })
}

// JSON renders the report as indented JSON.
func (r *Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Text renders the report for people.
func (r *Report) Text() string {
	var b strings.Builder
	c := r.Counts
	fmt.Fprintf(&b, "n8n import report: %q\n", r.Workflow)
	fmt.Fprintf(&b, "  nodes: %d mapped fully, %d partially, %d unmapped", c.Full, c.Partial, c.Unmapped)
	if c.Folded > 0 {
		fmt.Fprintf(&b, ", %d folded into their parent", c.Folded)
	}
	if c.Dropped > 0 {
		fmt.Fprintf(&b, ", %d dropped", c.Dropped)
	}
	fmt.Fprintf(&b, "\n  mono workflow: %d nodes, %d connections\n", c.MonoNodes, c.Connections)
	for _, n := range r.Nodes {
		if n.Status == StatusFull && len(n.Issues) == 0 {
			continue
		}
		target := n.MonoType
		if target == "" {
			target = "-"
		}
		fmt.Fprintf(&b, "\n  [%s] %s (%s → %s)\n", n.Status, n.Name, n.N8nType, target)
		for _, is := range n.Issues {
			line := is.Message
			if is.Param != "" {
				line = fmt.Sprintf("%s: %s", is.Param, is.Message)
				if is.Value != nil {
					line = fmt.Sprintf("%s = %s %s", is.Param, compactValue(is.Value), is.Message)
				}
			}
			fmt.Fprintf(&b, "    - %s: %s\n", is.Severity, line)
		}
		for _, e := range n.Expressions {
			fmt.Fprintf(&b, "    - expression kept in %s (%s): %s\n", e.Param, e.Reason, e.Expression)
		}
	}
	if len(r.Credentials) > 0 {
		b.WriteString("\n  connect these:\n")
		for _, cr := range r.Credentials {
			from := ""
			if cr.N8nName != "" {
				from = fmt.Sprintf(" (n8n credential %q, %s)", cr.N8nName, cr.N8nType)
			}
			fmt.Fprintf(&b, "    - %s%s for %s: %s\n", cr.Platform, from, strings.Join(cr.Nodes, ", "), cr.Hint)
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\n  note: %s", n)
	}
	if len(r.Notes) > 0 {
		b.WriteString("\n")
	}
	if r.Complete {
		b.WriteString("\n  result: complete — every node has a mono-agent equivalent\n")
	} else {
		fmt.Fprintf(&b, "\n  result: incomplete — %d node(s) are placeholders that fail until replaced\n", c.Unmapped)
	}
	return b.String()
}

// compactValue prints a parameter value on one short line.
func compactValue(v interface{}) string {
	var s string
	switch x := v.(type) {
	case string:
		s = fmt.Sprintf("%q", x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			s = fmt.Sprint(x)
		} else {
			s = string(b)
		}
	}
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
