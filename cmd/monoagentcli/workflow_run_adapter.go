package main

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/workflowrun"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// `workflow run`, `workflow templates run`, `workflow execution` and `workflow cancel` live in
// internal/cli/workflowrun; this is their adapter: the CLI's globals and helpers handed over as
// workflowrun.Deps, under the old names.

func workflowRunDeps(cfg *globalConfig) workflowrun.Deps {
	return workflowrun.Deps{
		JSONOutput: func() bool { return cfg.JSONOutput },
		ProfileID:  func() string { return cfg.ProfileID },
		OpenDB:     func() (*storage.Database, error) { return initDB(cfg) },
		BuildEngine: func() (*workflow.WorkflowEngine, func(), error) {
			return buildEngine(cfg, false)
		},
		NodeTypes: func() *workflow.NodeTypeRegistry { return buildNodeRegistry(cfg.Verbose, nil) },
		DryRun: func(cmd *cobra.Command, workflowID string) error {
			return runWorkflowDryRun(cfg, cmd, workflowID)
		},
	}
}

func newWorkflowRunCmd(cfg *globalConfig) *cobra.Command {
	return workflowRunDeps(cfg).NewRunCmd()
}

func newWorkflowTemplatesRunCmd(cfg *globalConfig) *cobra.Command {
	return workflowRunDeps(cfg).NewTemplatesRunCmd()
}

func newWorkflowExecutionCmd(cfg *globalConfig) *cobra.Command {
	return workflowRunDeps(cfg).NewExecutionCmd()
}

func newWorkflowCancelCmd(cfg *globalConfig) *cobra.Command {
	return workflowRunDeps(cfg).NewCancelCmd()
}

// checkTemplateNodeTypes refuses a template whose node types are not installed.
var checkTemplateNodeTypes = workflowrun.CheckTemplateNodeTypes
