package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ── ref nodes ─────────────────────────────────────────────────────────────────

func refNodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "List all node types grouped by category",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("╔══════════════════════════════════════════════════════════════╗")
			fmt.Println("║                  monoagentcli — node type reference                ║")
			fmt.Println("╚══════════════════════════════════════════════════════════════╝")
			fmt.Println()
			fmt.Printf("  %-38s  %s\n", "Type", "Description")
			fmt.Println("  " + strings.Repeat("─", 70))

			categories := []string{"trigger", "system", "core", "http", "data", "image", "vault", "db", "comm", "service", "applications", "agent", "org", "ai", "gemini", "instagram", "linkedin", "x", "tiktok", "hackernews", "producthunt", "people"}
			byCategory := make(map[string][]nodeDoc)
			for _, n := range nodeDocs {
				byCategory[n.Category] = append(byCategory[n.Category], n)
			}

			for _, cat := range categories {
				nodes := byCategory[cat]
				if len(nodes) == 0 {
					continue
				}
				fmt.Printf("\n  ── %s %s\n", strings.ToUpper(cat), strings.Repeat("─", max(0, 60-len(cat))))
				for _, n := range nodes {
					fmt.Printf("  %-38s  %s\n", n.Type, n.Short)
				}
			}
			fmt.Println()
			fmt.Println("  Run: monoagentcli ref node <type>   for full details on any node.")
		},
	}
}

// ── ref node <type> ───────────────────────────────────────────────────────────

func refNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node <type>",
		Short: "Show detailed documentation for a specific node type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ := args[0]
			d := findNodeDoc(typ)
			if d == nil {
				return fmt.Errorf("unknown node type %q — run 'monoagentcli ref nodes' to see all types", typ)
			}

			sep := strings.Repeat("═", 62)
			fmt.Println(sep)
			fmt.Printf("  NODE: %s\n", d.Type)
			fmt.Printf("  Category: %s\n", d.Category)
			fmt.Println(sep)
			fmt.Printf("\n  %s\n", d.Short)
			if d.Description != "" {
				fmt.Println()
				for _, line := range strings.Split(d.Description, "\n") {
					fmt.Printf("  %s\n", line)
				}
			}
			if d.Config != "" {
				fmt.Println("\n  Config example:")
				for _, line := range strings.Split(d.Config, "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
			if d.Inputs != "" {
				fmt.Printf("\n  Inputs:   %s\n", d.Inputs)
			}
			if d.Outputs != "" {
				fmt.Printf("  Outputs:  %s\n", d.Outputs)
			}
			if d.Notes != "" {
				fmt.Println("\n  Notes:")
				for _, line := range strings.Split(d.Notes, "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
			fmt.Println()
			return nil
		},
	}
}

// max returns the larger of two ints (Go 1.21 builtin not always available).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
