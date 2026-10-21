package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/monomind"
)

func withCaps(t *testing.T, caps ...string) {
	t.Helper()
	old := capabilityProbe
	capabilityProbe = func(*cobra.Command) (*monomind.CapabilitySet, error) {
		return monomind.NewCapabilitySet("9.0.0", caps...), nil
	}
	t.Cleanup(func() { capabilityProbe = old })
}

func lastExec(t *testing.T, argsLog string) string { return lastAgentExecLine(t, argsLog) }

// A page-read turn hands its images straight to `agent exec --attach`: no copy
// into any folder, and only when monomind takes the flag.
func TestPageReadTurnPassesImagesToTheRunner(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	_, argsLog, _, _ := writeCapturingMonomind(t)
	withCaps(t, "agent-exec", monomind.CapAgentExecAttachments)
	shot := writeFile(t, filepath.Join(t.TempDir(), "look.png"), pngHeader)

	if _, err := runChatCmd(t, dbPath, os.Getenv(monomind.EnvOverride), "--runtime", "claude", "--tools", "monoagent:read", "--attach", shot, "--", "what is drawn?"); err != nil {
		t.Fatal(err)
	}
	if line := lastExec(t, argsLog); !strings.Contains(line, "--attach "+shot) {
		t.Errorf("the image was not passed to the runner: %s", line)
	}
}

// Without agent-exec-attachments the turn is refused with a clear message,
// before the agent runs: a model that cannot see the image must not be asked
// about it.
func TestPageReadImagesRefusedWithoutRunnerSupport(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	_, argsLog, _, _ := writeCapturingMonomind(t)
	withCaps(t, "agent-exec")
	shot := writeFile(t, filepath.Join(t.TempDir(), "look.png"), pngHeader)

	_, err := runChatCmd(t, dbPath, os.Getenv(monomind.EnvOverride), "--runtime", "claude", "--tools", "monoagent:read", "--attach", shot, "--", "hi")
	if exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "agent-exec-attachments") {
		t.Fatalf("err = %v, want exit 3 naming agent-exec-attachments", err)
	}
	if logged, _ := os.ReadFile(argsLog); strings.Contains(string(logged), "agent exec") {
		t.Error("the agent ran although the image could not be passed")
	}
}

// Only a few small pictures: a file that is not an image (by its first bytes,
// whatever its name), a folder, too many, and a big one are all refused.
func TestPageReadImagesRefusals(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	_, argsLog, _, _ := writeCapturingMonomind(t)
	withCaps(t, "agent-exec", monomind.CapAgentExecAttachments)
	dir := t.TempDir()
	png := writeFile(t, filepath.Join(dir, "a.png"), pngHeader)
	fake := writeFile(t, filepath.Join(dir, "fake.png"), []byte("#!/bin/sh\necho secret\n"))
	big := writeFile(t, filepath.Join(dir, "big.png"), append(append([]byte{}, pngHeader...), make([]byte, pageReadImageBytes)...))
	cases := map[string][]string{
		"not an image":   {fake},
		"folder":         {dir},
		"too large":      {big},
		"too many":       {png, png, png, png},
		"missing":        {filepath.Join(dir, "none.png")},
		"one bad of two": {png, fake},
	}
	for name, files := range cases {
		args := []string{"--runtime", "claude", "--tools", "monoagent:read"}
		for _, f := range files {
			args = append(args, "--attach", f)
		}
		args = append(args, "--", "hi")
		if _, err := runChatCmd(t, dbPath, os.Getenv(monomind.EnvOverride), args...); exitCodeFor(err) != 3 {
			t.Errorf("%s: err = %v, want exit 3", name, err)
		}
	}
	if logged, _ := os.ReadFile(argsLog); strings.Contains(string(logged), "agent exec") {
		t.Error("a refused attachment still ran the agent")
	}
}

// The journaled turn the extension runs (--conversation/--turn) takes images
// too, though an assistant conversation takes no other --attach.
func TestJournaledPageReadTurnTakesImages(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	bin, argsLog, _, _ := writeCapturingMonomind(t)
	withCaps(t, "agent-exec", monomind.CapAgentExecAttachments)
	store := openTestChatStore(t, dbPath)
	conv, err := store.CreateConversation("default", "agent", "general", "claude", "", "")
	if err != nil {
		t.Fatal(err)
	}
	shot := writeFile(t, filepath.Join(t.TempDir(), "look.png"), pngHeader)
	if out, err := runChatCmd(t, dbPath, bin, "--conversation", conv.ID, "--turn", "t-1", "--tools", "monoagent:read", "--attach", shot, "--", "what is drawn?"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if line := lastExec(t, argsLog); !strings.Contains(line, "--attach "+shot) {
		t.Errorf("the image was not passed to the runner: %s", line)
	}
	// Without the page-read mode an assistant chat still takes no --attach.
	if _, err := runChatCmd(t, dbPath, bin, "--conversation", conv.ID, "--turn", "t-2", "--attach", shot, "--", "hi"); exitCodeFor(err) != 3 {
		t.Errorf("assistant --attach outside page-read: err = %v, want exit 3", err)
	}
}
