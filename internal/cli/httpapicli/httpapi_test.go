package httpapicli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/cli/apiruntime"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent or the OS keychain, and none of them starts a server:
// each run is refused before anything listens.
func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

type invalidInput struct{ msg string }

func (e *invalidInput) Error() string { return e.msg }

func testDeps(t *testing.T, served *bool) Deps {
	dbPath := testdb.Path(t)
	return Deps{
		OpenGatewayDB: func() (*storage.Database, error) { return storage.NewDatabase(dbPath) },
		DBPath:        func() string { return dbPath },
		Version:       func() string { return "test" },
		API: apiruntime.Deps{
			Version:      func() string { return "test" },
			InvalidInput: func(f string, a ...interface{}) error { return &invalidInput{fmt.Sprintf(f, a...)} },
			AsCLIError:   func(err error) error { return err },
		},
		Serving: func(c *cobra.Command) *cobra.Command { *served = true; return c },
	}
}

func TestHTTPAPICommand(t *testing.T) {
	t.Parallel()
	var served bool
	cmd := NewCommand(testDeps(t, &served))
	if !served {
		t.Error("httpapi is not marked as a serving command")
	}
	for _, name := range []string{"addr", "allow-mutations", "v1-addr", "confinement", "context-confinement", "auto-confinement", "max-concurrent"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("no --%s", name)
		}
	}
}

// A bad API flag is refused as invalid input before any listener is opened.
func TestHTTPAPIRefusesABadAPIFlagBeforeListening(t *testing.T) {
	t.Parallel()
	var served bool
	cmd := NewCommand(testDeps(t, &served))
	cmd.SetArgs([]string{"--max-concurrent", "999", "--addr", "127.0.0.1:1"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.Execute()
	var in *invalidInput
	if !errors.As(err, &in) || !strings.Contains(err.Error(), "--max-concurrent") {
		t.Fatalf("err = %v, want invalid input about --max-concurrent", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refused start printed %q", out.String())
	}
}

func TestMutationsLabel(t *testing.T) {
	t.Parallel()
	if mutationsLabel(true) != "enabled" || mutationsLabel(false) != "disabled — read-only" {
		t.Error(mutationsLabel(true), mutationsLabel(false))
	}
}

// The server serves every profile, so --profile means nothing to it: it is
// accepted (scripts that carry it keep working) and the command says so once.
func TestHTTPAPIIgnoresProfileAndSaysSo(t *testing.T) {
	t.Parallel()
	// run executes httpapi with a bad flag, so it is refused before anything listens.
	run := func(args ...string) string {
		var served bool
		root := &cobra.Command{Use: "monoagentcli"}
		root.PersistentFlags().String("profile", "", "")
		root.AddCommand(NewCommand(testDeps(t, &served)))
		root.SetArgs(append(args, "httpapi", "--max-concurrent", "999", "--addr", "127.0.0.1:1"))
		var errOut bytes.Buffer
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&errOut)
		root.SilenceErrors, root.SilenceUsage = true, true
		if err := root.Execute(); err == nil {
			t.Fatal("the bad flag was accepted")
		}
		return errOut.String()
	}
	if got := run("--profile", "work"); strings.Count(got, "--profile is ignored") != 1 {
		t.Errorf("stderr %q should carry the note once", got)
	}
	if got := run(); strings.Contains(got, "--profile") {
		t.Errorf("without --profile stderr says %q", got)
	}
}
