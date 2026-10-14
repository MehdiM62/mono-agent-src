package workflowrun

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// What a human-mode run shows of its result; --json has everything.
const (
	runOutputMaxItems = 5
	runOutputMaxLines = 40
)

// printRunOutput shows what a finished run produced: the output items of
// its leaf nodes (no outgoing connection) that succeeded, in the order they
// ran, as pretty JSON, so `templates run hello` ends with its result rather
// than a bare "Status: SUCCESS". Items are the summary's, so already
// redacted and truncated. Nothing is printed when no leaf produced output.
func printRunOutput(w io.Writer, s executionSummaryJSON, wf *workflow.Workflow) {
	if wf == nil || (s.Status != "SUCCESS" && s.Status != "SUCCESS_WITH_ERRORS") {
		return
	}
	hasNext := map[string]bool{}
	for _, c := range wf.Connections {
		hasNext[c.SourceNodeID] = true
	}
	names := map[string]string{}
	for _, n := range wf.Nodes {
		names[n.ID] = n.Name
	}
	var items []workflow.Item
	var from []string
	for _, n := range s.Nodes {
		if n.Status != "SUCCESS" || hasNext[n.NodeID] || strings.HasPrefix(n.Type, "trigger.") || len(n.OutputItems) == 0 {
			continue
		}
		items = append(items, n.OutputItems...)
		from = append(from, names[n.NodeID])
	}
	if len(items) == 0 {
		return
	}

	fmt.Fprintf(w, "\nOutput (%s):\n", strings.Join(from, ", "))
	lines, shown, cut := 0, 0, false
	for _, it := range items {
		if shown == runOutputMaxItems || lines >= runOutputMaxLines {
			break
		}
		b, err := json.MarshalIndent(it.JSON, "", "  ")
		if err != nil {
			break
		}
		ls := strings.Split(string(b), "\n")
		if room := runOutputMaxLines - lines; len(ls) > room {
			ls, cut = ls[:room], true
		}
		fmt.Fprintln(w, strings.Join(ls, "\n"))
		lines += len(ls)
		shown++
	}
	switch rest := len(items) - shown; {
	case rest > 0:
		fmt.Fprintf(w, "… %d more items (use --json for all)\n", rest)
	case cut:
		fmt.Fprintln(w, "… (use --json for all)")
	}
}
