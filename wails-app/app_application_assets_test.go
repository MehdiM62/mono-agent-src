//go:build !windows

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetApplicationAssetsShellsOutAndKeepsFieldNames(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo "$*" >> '`+log+`'
echo '{"application":{},"status_log":[],"assets":{"posting":{"url":"https://x.test/j","capturedText":"hi","capturedAt":"c","deadline":"d"},"documents":[{"id":"doc-1","filename":"cv.pdf","kind":"cv","source":"upload","format":"pdf","sizeBytes":7,"createdAt":"c","attached":true,"path":"/v/doc-1.pdf"}],"fill":{"status":"filled","screenshot":"/s.png","updatedAt":"u","fields":[{"label":"Email","value":"a@b","source":"profile","generated":true}],"note":"n"},"people":[{"id":"p1","name":"Jo","role":"HR","email":"jo@x"}],"messages":null,"tasks":[{"id":"1","title":"T","status":"ready"}],"task":{"taskId":"t1","conversationId":"c1","status":"running"},"links":[{"kind":"person","refId":"p1"}]}}'
`))
	a := newTestApp(t)
	a.ctx = context.Background()
	a.selectForTest("work")

	got, err := a.GetApplicationAssets("app-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Posting.CapturedText != "hi" || got.Documents[0].SizeBytes != 7 || !got.Documents[0].Attached ||
		got.Fill.Status != "filled" || !got.Fill.Fields[0].Generated || got.People[0].Email != "jo@x" ||
		got.Task == nil || got.Task.ConversationID != "c1" || got.Links[0].RefID != "p1" || got.Tasks[0].Title != "T" {
		t.Fatalf("assets = %+v", got)
	}
	if got.Messages == nil {
		t.Fatal("messages is nil, the page expects an empty list")
	}
	want := "--profile work --json application show app-1 --assets"
	if calls := loggedArgs(t, log); strings.Join(calls, "\n") != want {
		t.Fatalf("CLI calls = %v, want %q", calls, want)
	}
}

func TestGetApplicationAssetsSurfacesCLIFailure(t *testing.T) {
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, "echo 'application nope not found' >&2; exit 2\n"))
	a := newTestApp(t)
	a.ctx = context.Background()
	if _, err := a.GetApplicationAssets("nope"); err == nil {
		t.Fatal("CLI failure was hidden")
	}
}
