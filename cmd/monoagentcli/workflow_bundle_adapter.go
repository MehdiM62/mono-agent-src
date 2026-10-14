package main

import (
	"io"

	"github.com/monoes/mono-agent/internal/automation"
	"github.com/monoes/mono-agent/internal/cli/workflowbundle"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Workflow bundles (`workflow export --bundle-automations`, the bundle half of `workflow import`,
// and `library publish`) live in internal/cli/workflowbundle. This file hands it the CLI's
// helpers (workflowio takes them as workflowBundleDeps) and keeps the old names that
// library_publish.go and the bundle tests use.

// workflowBundleDeps hands the CLI's helpers to internal/cli/workflowbundle.
func workflowBundleDeps() workflowbundle.Deps {
	return workflowbundle.Deps{
		OpenRegistry:            openAutomationRegistry,
		InvalidInput:            errInvalidInput,
		ErrConfirmationRequired: errConfirmationRequired,
	}
}

type (
	bundledAutomation  = workflowbundle.BundledAutomation
	workflowBundleFile = workflowbundle.BundleFile
	bundleImportItem   = workflowbundle.ImportItem
)

// bundleOptions controls which site domains an exported package gets
// (workflowbundle.Options).
type bundleOptions struct {
	domains      map[string][]string
	useSuggested bool
	skipShipped  bool
}

// bundleImportOptions controls handleBundledAutomations (workflowbundle.ImportOptions).
type bundleImportOptions struct {
	yes         bool
	replace     bool
	interactive bool
	in          io.Reader
	out         io.Writer
}

func bundleWorkflowAutomations(file workflow.WorkflowFile, opts bundleOptions) (workflowBundleFile, error) {
	return workflowBundleDeps().BundleWorkflowAutomations(file, workflowbundle.Options{
		Domains: opts.domains, UseSuggested: opts.useSuggested, SkipShipped: opts.skipShipped})
}

func handleBundledAutomations(raw []byte, o bundleImportOptions) []bundleImportItem {
	return workflowBundleDeps().HandleBundledAutomations(raw, workflowbundle.ImportOptions{
		Yes: o.yes, Replace: o.replace, Interactive: o.interactive, In: o.in, Out: o.out})
}

func newBundleReviewDetail(r *automation.InstallResult) *workflowbundle.ReviewDetail {
	return workflowbundle.NewReviewDetail(r)
}

var (
	suggestedNotices = workflowbundle.SuggestedNotices
	packageResolver  = workflowbundle.PackageResolver
	writeFileAtomic  = workflowbundle.WriteFileAtomic // library_api.go
)
