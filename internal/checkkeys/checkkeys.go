// Package checkkeys finds the keys the app nodes of workflow or template
// files send that their ops don't declare (apipkg.UndeclaredIn over the
// built-in catalog): the ones an agent's run, or an unapproved workflow's,
// of such a node waits on. It reads the files it is given and the catalog,
// nothing else, for `workflow check-keys` and scripts/checkkeys alike.
package checkkeys

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/monoes/mono-agent/internal/apipkg"
)

// Finding is one key an app node's config sends that its op doesn't
// declare: a key of a collection the request merges whole, a key of an
// option object, or either given as an expression.
type Finding struct {
	File   string `json:"file"`
	NodeID string `json:"node_id"`
	Node   string `json:"node,omitempty"`
	Type   string `json:"type"`
	Op     string `json:"op"`
	Key    string `json:"key"`
}

// Report is what a check found, and the --json document.
type Report struct {
	Clean    bool      `json:"clean"`
	Files    int       `json:"files"`
	AppNodes int       `json:"app_nodes"`
	Findings []Finding `json:"findings"`
	Errors   []string  `json:"errors,omitempty"`
}

// Exit codes: clean, an undeclared key, and a file (or the catalog) that
// couldn't be checked.
const (
	ExitClean      = 0
	ExitUndeclared = 1
	ExitBadInput   = 3
)

// MaxInput caps one input: a workflow or template is far smaller, and a
// path to /dev/zero must not fill the memory.
const MaxInput = 16 << 20

// Check reads each of names ("-" is stdin; none means stdin) and reports
// its app nodes' undeclared keys. A file that can't be read (not a regular
// file, or over MaxInput) or isn't JSON is an entry of Errors, and the
// others are still checked; the error is for a catalog that doesn't load.
func Check(stdin io.Reader, names []string) (Report, error) {
	pkgs, err := apipkg.Catalog()
	if err != nil {
		return Report{}, fmt.Errorf("load the API catalog: %w", err)
	}
	if len(names) == 0 {
		names = []string{"-"}
	}
	rep := Report{Findings: []Finding{}}
	for _, name := range names {
		raw, err := read(stdin, name)
		var v any
		if err == nil {
			err = notJSON(json.Unmarshal(raw, &v))
		}
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		rep.Files++
		for _, n := range nodesIn(v) {
			op, keys := apipkg.UndeclaredIn(pkgs, n.nodeType, n.config)
			if op == "" {
				continue
			}
			rep.AppNodes++
			for _, k := range keys {
				rep.Findings = append(rep.Findings, Finding{File: name, NodeID: n.id, Node: n.name, Type: n.nodeType, Op: op, Key: k})
			}
		}
	}
	rep.Clean = len(rep.Findings) == 0 && len(rep.Errors) == 0
	return rep, nil
}

// read is name's bytes ("-" is stdin), up to MaxInput: a regular file only,
// opened without blocking, since a FIFO would wait for a writer forever.
func read(stdin io.Reader, name string) ([]byte, error) {
	r := stdin
	if name != "-" {
		f, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		fi, err := f.Stat()
		switch {
		case err != nil:
			return nil, err
		case !fi.Mode().IsRegular():
			return nil, errors.New("not a regular file")
		case fi.Size() > MaxInput:
			return nil, tooLarge
		}
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if err == nil && len(b) > MaxInput {
		err = tooLarge
	}
	return b, err
}

var tooLarge = fmt.Errorf("larger than %d MB", MaxInput>>20)

// notJSON says where a decode failed without echoing the input: the
// decoder's own message quotes the character it stopped at.
func notJSON(err error) error {
	var syn *json.SyntaxError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &syn):
		return fmt.Errorf("not valid JSON (at byte %d)", syn.Offset)
	}
	return errors.New("not valid JSON")
}

// Write prints r to w: the JSON document, or a line per finding and a
// summary.
func (r Report) Write(w io.Writer, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(w).Encode(r)
		return
	}
	for _, f := range r.Findings {
		name := ""
		if f.Node != "" {
			name = fmt.Sprintf(" (%s)", f.Node)
		}
		fmt.Fprintf(w, "%s: node %q%s, %s %s: undeclared %s\n", f.File, f.NodeID, name, f.Type, f.Op, f.Key)
	}
	fmt.Fprintf(w, "%d file(s), %d app node(s): %d undeclared key(s)\n", r.Files, r.AppNodes, len(r.Findings))
}

// ExitCode is r's exit code: ExitBadInput when a file couldn't be checked,
// else ExitUndeclared when a node has an undeclared key, else ExitClean.
func (r Report) ExitCode() int {
	switch {
	case len(r.Errors) > 0:
		return ExitBadInput
	case len(r.Findings) > 0:
		return ExitUndeclared
	}
	return ExitClean
}

// Reason says why r doesn't exit clean: the files it couldn't check, or how
// many keys it found; "" when clean.
func (r Report) Reason() string {
	switch r.ExitCode() {
	case ExitBadInput:
		return strings.Join(r.Errors, "\n")
	case ExitUndeclared:
		return fmt.Sprintf("%d undeclared key(s) in %d file(s)", len(r.Findings), r.Files)
	}
	return ""
}

// node is one workflow node a file holds.
type node struct {
	id, name, nodeType string
	config             map[string]any
}

// nodesIn finds the nodes of every workflow in v, however deep (a template
// wrapping one, an org's workflows): an object whose "nodes" lists typed
// nodes ("type", or the legacy "node_type").
func nodesIn(v any) []node {
	var out []node
	switch x := v.(type) {
	case map[string]any:
		if list, ok := x["nodes"].([]any); ok {
			for _, e := range list {
				n, _ := e.(map[string]any)
				t, _ := n["type"].(string)
				if t == "" {
					t, _ = n["node_type"].(string)
				}
				if t == "" {
					continue
				}
				id, _ := n["id"].(string)
				name, _ := n["name"].(string)
				cfg, _ := n["config"].(map[string]any)
				out = append(out, node{id, name, t, cfg})
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, nodesIn(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, nodesIn(e)...)
		}
	}
	return out
}
