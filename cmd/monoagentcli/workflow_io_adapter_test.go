package main

import (
	"context"
	"strings"
	"testing"
)

// TestWorkflowIOAdapterWiring pins what workflowIODeps hands internal/cli/workflowio: --json
// failures as one {"error","code"} document on stdout (withJSONErrors), the real store and
// ownedWorkflow, and the old names other main files use (parseWorkflowDefinition for validate
// and library install, workflowFileFromWorkflow for library publish). Import, export, convert
// and bundles end to end are workflow_test.go, workflow_import_*_test.go,
// workflow_persist_test.go and workflow_bundle_*_test.go; that a reported n8n failure is not
// printed twice is TestWorkflowConvertN8nJSONAndExitCodes.
func TestWorkflowIOAdapterWiring(t *testing.T) {
	_, cfg := persistTestEnv(t)
	out, err := runWorkflowCmdIn(t, cfg, "{", "import")
	if exitCodeFor(err) != 3 || !strings.HasPrefix(out, `{"code":"invalid_input","error":"parse workflow JSON: `) {
		t.Fatalf("bad JSON: exit %d, stdout %q", exitCodeFor(err), out)
	}
	if _, err := parseWorkflowDefinition([]byte(`{"nodes":[{"id":"x"}]}`)); exitCodeFor(err) != 3 {
		t.Fatalf("node without a type: exit %d (%v), want 3", exitCodeFor(err), err)
	}

	saved := saveDoc(t, cfg, editorDoc)
	db, err := initDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := openWorkflowStore(db)
	wf := ownedWorkflow(context.Background(), store, db.DB, "default", saved.ID)
	if wf == nil || ownedWorkflow(context.Background(), store, db.DB, "other", saved.ID) != nil {
		t.Fatal("ownedWorkflow does not keep the profile boundary")
	}
	if f := workflowFileFromWorkflow(wf); len(f.Nodes) != 2 || f.Connections[0].Source != "ed-n1" {
		t.Fatalf("file = %+v", f)
	}
	if out, err = runWorkflowCmdIn(t, cfg, "", "export", saved.ID); err != nil || !strings.Contains(out, `"source": "ed-n1"`) {
		t.Fatalf("export = %q, %v", out, err)
	}
}
