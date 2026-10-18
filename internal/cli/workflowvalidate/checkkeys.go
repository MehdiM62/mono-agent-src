package workflowvalidate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// KeyFinding is one key an app node's config sends that its op doesn't
// declare (apipkg.Undeclared): a key of a collection the request merges
// whole, a key of an option object, or either given as an expression.
type KeyFinding struct {
	File   string `json:"file"`
	NodeID string `json:"node_id"`
	Node   string `json:"node,omitempty"`
	Type   string `json:"type"`
	Op     string `json:"op"`
	Key    string `json:"key"`
}

// CheckKeysJSON is the `workflow check-keys --json` payload.
type CheckKeysJSON struct {
	Clean    bool         `json:"clean"`
	Files    int          `json:"files"`
	AppNodes int          `json:"app_nodes"`
	Findings []KeyFinding `json:"findings"`
	Errors   []string     `json:"errors,omitempty"`
}

// NewCheckKeysCommand is `workflow check-keys`: the keys the app nodes of
// workflow or template files send that their ops don't declare, judged on
// the built-in catalog. An agent's run, or an unapproved workflow's, of such
// a node waits for approval. It reads the files (or stdin) and nothing else:
// no database, no ~/.monoagent, no network.
func NewCheckKeysCommand(jsonOutput func() bool) *cobra.Command {
	return &cobra.Command{
		Use:   "check-keys [file.json…|-]",
		Short: "List the keys app nodes send that their operations don't declare (no database, no state)",
		Long: "Reads workflow or template JSON files (stdin with - or no file) and, for every app node in them " +
			"(a workflow nested in a template or an org included), lists the keys its config sends that its " +
			"operation doesn't declare, judged on the built-in API catalog: a key of a collection the request " +
			"merges whole, a key of an option object, or either given as an expression. An agent's run, or an " +
			"unapproved workflow's, of such a node waits for approval. It opens no database and reads nothing " +
			"under ~/.monoagent (installed packages included), so it runs in a bare environment. --json prints " +
			"{clean, files, app_nodes, findings[{file, node_id, node, type, op, key}], errors}. " +
			"Exit 0 when clean, 1 when a node has an undeclared key, 3 when a file can't be read or isn't JSON " +
			"(the other files are still checked).",
		Example: `  monoagentcli workflow check-keys library/*.json
  monoagentcli --json workflow check-keys template.json
  cat template.json | monoagentcli workflow check-keys -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgs, err := apipkg.Catalog()
			if err != nil {
				return fmt.Errorf("load the API catalog: %w", err)
			}
			if len(args) == 0 {
				args = []string{"-"}
			}
			rep := CheckKeysJSON{Findings: []KeyFinding{}}
			for _, name := range args {
				raw, err := readInput(cmd.InOrStdin(), name)
				var v any
				if err == nil {
					err = json.Unmarshal(raw, &v)
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
						rep.Findings = append(rep.Findings, KeyFinding{File: name, NodeID: n.id, Node: n.name, Type: n.nodeType, Op: op, Key: k})
					}
				}
			}
			rep.Clean = len(rep.Findings) == 0 && len(rep.Errors) == 0
			return reportCheckKeys(cmd, rep, jsonOutput())
		},
	}
}

func readInput(stdin io.Reader, name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(name)
}

// reportCheckKeys prints rep and returns the exit-code error: 3 when a
// file couldn't be checked, 1 when a node has an undeclared key.
func reportCheckKeys(cmd *cobra.Command, rep CheckKeysJSON, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		_ = json.NewEncoder(out).Encode(rep)
	} else {
		for _, f := range rep.Findings {
			name := ""
			if f.Node != "" {
				name = fmt.Sprintf(" (%s)", f.Node)
			}
			fmt.Fprintf(out, "%s: node %q%s, %s %s: undeclared %s\n", f.File, f.NodeID, name, f.Type, f.Op, f.Key)
		}
		fmt.Fprintf(out, "%d file(s), %d app node(s): %d undeclared key(s)\n", rep.Files, rep.AppNodes, len(rep.Findings))
	}
	var err error
	switch {
	case len(rep.Errors) > 0:
		err = clicore.InvalidInput("%s", strings.Join(rep.Errors, "\n"))
	case len(rep.Findings) > 0:
		err = &clicore.Error{Code: 1, Msg: fmt.Sprintf("%d undeclared key(s) in %d file(s)", len(rep.Findings), rep.Files)}
	default:
		return nil
	}
	if asJSON {
		return clicore.Reported{Err: err}
	}
	if len(rep.Errors) == 0 {
		return clicore.Shown{Err: err}
	}
	return err
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
