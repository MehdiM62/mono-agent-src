package data

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

// The file-writing nodes go through fsconfine.WritePath: in a workflow the
// user never approved they write no hidden file in the home outside the
// project folder, and nothing is written; inside the project they write.
func TestFileNodesWriteNoHiddenHomeFilesForAnUnapprovedWorkflow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "work")
	ctx := workflow.WithUnapproved(context.Background(), project)
	in := workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"text": "echo hi"})}}
	write := func(path string) error {
		_, err := (&WriteBinaryFileNode{}).Execute(ctx, in, map[string]interface{}{"file_path": path, "field": "text", "encoding": "utf8"})
		return err
	}
	rc := filepath.Join(home, ".bashrc")
	if err := write(rc); !errors.Is(err, fsconfine.ErrHiddenHomeWrite) {
		t.Fatalf("write_binary_file to ~/.bashrc: %v", err)
	}
	csv := map[string]interface{}{"operation": "write_csv", "file_path": filepath.Join(home, ".config", "x.csv")}
	if _, err := (&SpreadsheetNode{}).Execute(ctx, in, csv); !errors.Is(err, fsconfine.ErrHiddenHomeWrite) {
		t.Fatalf("spreadsheet write_csv to ~/.config: %v", err)
	}
	if _, err := os.Stat(rc); err == nil {
		t.Fatal("~/.bashrc was written")
	}
	if err := write(filepath.Join(project, ".scripts", "run.sh")); err != nil {
		t.Fatalf("a hidden folder in the project: %v", err)
	}
}
