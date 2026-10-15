package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/autostart"
	"github.com/monoes/mono-agent/internal/storage"
)

type recordingInstaller struct{ profiles []string }

func (r *recordingInstaller) Install(_ context.Context, profileID string) (autostart.Result, error) {
	r.profiles = append(r.profiles, profileID)
	return autostart.Result{Description: "installed"}, nil
}
func (*recordingInstaller) Uninstall(context.Context) error       { return nil }
func (*recordingInstaller) Status(context.Context) (bool, string) { return false, "" }
func (*recordingInstaller) Start(context.Context) error           { return nil }
func (*recordingInstaller) Restart(context.Context) error         { return nil }

// runInstall runs `daemon install` under a root that has the global --profile flag.
func runInstall(t *testing.T, rec *recordingInstaller, resolved string, args ...string) error {
	t.Helper()
	root := &cobra.Command{Use: "monoagentcli"}
	root.PersistentFlags().String("profile", "", "")
	root.AddCommand(NewCommand(Deps{
		ProfileID:    func() string { return resolved },
		OpenDB:       func() (*storage.Database, error) { return storage.NewDatabase(t.TempDir() + "/t.db") },
		NewInstaller: func() autostart.Installer { return rec },
	}))
	root.SetArgs(append([]string{"daemon", "install"}, args...))
	root.SilenceErrors, root.SilenceUsage = true, true
	return root.Execute()
}

// The registered service always names its profile; install never picks one for the user.
func TestInstallRequiresAnExplicitProfile(t *testing.T) {
	rec := &recordingInstaller{}
	err := runInstall(t, rec, "p-1")
	if err == nil || !strings.Contains(err.Error(), "--profile") || len(rec.profiles) != 0 {
		t.Fatalf("install without --profile = %v, installs %v", err, rec.profiles)
	}
}

func TestInstallRegistersTheResolvedProfile(t *testing.T) {
	rec := &recordingInstaller{}
	if err := runInstall(t, rec, "p-1", "--profile", "Work"); err != nil {
		t.Fatal(err)
	}
	if len(rec.profiles) != 1 || rec.profiles[0] != "p-1" {
		t.Fatalf("installed for %v, want the resolved id p-1", rec.profiles)
	}
}
