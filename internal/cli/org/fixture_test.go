package org

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testhome"
	"github.com/monoes/mono-agent/internal/workflow"
)

// TestMain keeps the tests out of the real home and mocks the keychain, as
// cmd/monoagentcli's does.
func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

// invalidInputError is the fake of the CLI's invalid-input error (exit 3).
type invalidInputError struct{ msg string }

func (e *invalidInputError) Error() string { return e.msg }

func testDeps() Deps {
	return Deps{
		InvalidInput: func(format string, a ...interface{}) error {
			return &invalidInputError{msg: fmt.Sprintf(format, a...)}
		},
	}
}

// testEnv is the org environment on the database at dbPath (migrated on
// first use, as initDB does) with profile profileID and --project *project.
func testEnv(dbPath, profileID string, project *string) *orgEnv {
	if project == nil {
		project = new(string)
	}
	return newEnv(testDeps(), orgenv.New(orgenv.Deps{
		OpenDB: func() (*storage.Database, error) {
			db, err := storage.NewDatabase(dbPath)
			if err != nil {
				return nil, err
			}
			if err := db.ApplyMigrations(); err != nil {
				db.Close()
				return nil, err
			}
			return db, nil
		},
		ProfileID:         func() string { return profileID },
		OpenWorkflowStore: openTestWorkflowStore,
		Project:           func() string { return *project },
	}))
}

// openTestWorkflowStore is the CLI's workflow store: files under
// ~/.monoagent/workflows plus SQLite.
func openTestWorkflowStore(db *storage.Database) *workflow.HybridWorkflowStore {
	home, _ := os.UserHomeDir()
	files, err := workflow.NewWorkflowFileStore(filepath.Join(home, ".monoagent", "workflows"))
	if err != nil {
		return workflow.NewHybridWorkflowStore(nil, workflow.NewSQLiteWorkflowStore(db.DB))
	}
	return workflow.NewHybridWorkflowStore(files, workflow.NewSQLiteWorkflowStore(db.DB))
}

// testOrgCmd is an `org` command with --project and this package's
// subcommands on a database at dbPath, profile "default".
func testOrgCmd(dbPath string) *cobra.Command {
	project := new(string)
	env := testEnv(dbPath, "default", project)
	cmd := &cobra.Command{
		Use:               "org",
		PersistentPostRun: func(cmd *cobra.Command, args []string) { env.Close() },
		SilenceUsage:      true,
		SilenceErrors:     true,
	}
	cmd.PersistentFlags().StringVar(project, "project", "", "")
	cmd.AddCommand(
		newOrgAutomationCmd(env),
		newOrgGrantCmd(env),
		newOrgEffectiveToolsCmd(env),
		newOrgAutonomyCmd(env),
		newOrgSummaryCmd(env),
		newOrgAutomationRoleCmd(env),
		newOrgGroupCmd(env),
		newOrgReconcileDocCmd(env),
	)
	return cmd
}

// captureStdout redirects os.Stdout while fn runs and returns what was
// written: the org commands print with fmt.Println.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func writeOrgFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, ".monomind", "orgs", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type orgCLIFixture struct {
	dbPath     string
	root       string
	outboundWF string
	plainWF    string
}

// newOrgCLIFixture isolates HOME (profile folders, workflow file store,
// daemon heartbeat), points monomind at the fake script, and seeds one org
// plus two workflows: one with an outbound node, one without.
func newOrgCLIFixture(t *testing.T) *orgCLIFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONOAGENT_DAEMON_HEARTBEAT", filepath.Join(home, "hb.json"))
	t.Setenv("MONOAGENT_API_ADDR", "")
	fake, err := filepath.Abs(filepath.Join("..", "..", "monomind", "testdata", "fake-monomind.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONOMIND_BIN", fake)

	dbPath := filepath.Join(home, "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	store := workflow.NewSQLiteWorkflowStore(db.DB)
	ctx := context.Background()
	mk := func(name string, nodeType string) string {
		wf := &workflow.Workflow{Name: name, ProfileID: "default"}
		if err := store.CreateWorkflow(ctx, wf); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveWorkflowNodes(ctx, wf.ID, []workflow.WorkflowNode{
			{ID: wf.ID + "-t", WorkflowID: wf.ID, Type: "trigger.manual", Name: "start"},
			{ID: wf.ID + "-n", WorkflowID: wf.ID, Type: nodeType, Name: "step"},
		}); err != nil {
			t.Fatal(err)
		}
		return wf.ID
	}
	f := &orgCLIFixture{
		dbPath:     dbPath,
		root:       profiledir.Root(db.DB, "default"),
		outboundWF: mk("Publish post", "comm.slack"),
		plainWF:    mk("Summarize", "core.set"),
	}
	db.Close()

	doc := orgdesign.NewOrg("growth", "grow", orgdesign.NewOrgOptions{})
	reportsTo := "lead"
	doc.Roles = append(doc.Roles, orgdesign.Role{ID: "writer", Title: "Writer", Type: "specialist", ReportsTo: &reportsTo, Responsibilities: []string{}})
	if _, err := orgdesign.Save(f.root, doc); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *orgCLIFixture) run(t *testing.T, args ...string) (map[string]interface{}, error) {
	t.Helper()
	cmd := testOrgCmd(f.dbPath)
	cmd.SetArgs(args)
	var runErr error
	out := captureStdout(t, func() { runErr = cmd.Execute() })
	if runErr != nil {
		return nil, runErr
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("org %v printed non-JSON %q: %v", args, out, err)
	}
	return m, nil
}

func (f *orgCLIFixture) mustRun(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	m, err := f.run(t, args...)
	if err != nil {
		t.Fatalf("org %v: %v", args, err)
	}
	return m
}

// depsEnv is an environment for helpers that only build errors.
func depsEnv() *orgEnv { return newEnv(testDeps(), nil) }
