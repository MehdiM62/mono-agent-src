package main

import (
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Every open of the database, the CLI's, the daemon's and the MCP server's
// alike, approves once the workflow definitions there were before approvals
// existed (internal/storage can't import internal/workflow).
func init() { storage.RegisterAfterMigrations(workflow.BackfillApprovals) }
