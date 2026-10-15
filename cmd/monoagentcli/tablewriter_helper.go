// cmd/monoagentcli/tablewriter_helper.go
package main

import (
	"io"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"

	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// newPlainTable builds a table in this CLI's borderless, non-wrapping look
// (clicore.NewPlainTable).
func newPlainTable(w io.Writer, headers []string, perColumnAlign []tw.Align) *tablewriter.Table {
	return clicore.NewPlainTable(w, headers, perColumnAlign)
}
