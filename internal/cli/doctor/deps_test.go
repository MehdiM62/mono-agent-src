package doctor

import (
	"context"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/health"
)

// doctor's --project reaches the environment the checks run in; setup has no --project.
func TestProjectFlagReachesNewEnv(t *testing.T) {
	t.Parallel()
	var got []string
	d := testDeps(true)
	newEnv := d.NewEnv
	d.NewEnv = func(projects []string) (*health.Env, func()) { got = projects; return newEnv(projects) }
	cmd := newDoctorCmd(d)
	if err := cmd.ParseFlags([]string{"--project", "codes/app,site"}); err != nil {
		t.Fatal(err)
	}
	runHealth(context.Background(), d, health.NewRegistry(nil, nil), health.Options{})
	if want := []string{"codes/app", "site"}; !reflect.DeepEqual(got, want) {
		t.Errorf("projects = %v, want %v", got, want)
	}
	if NewSetupCommand(*testDeps(false)).Flags().Lookup("project") != nil {
		t.Error("setup has --project")
	}
	if f := NewDoctorCommand(*testDeps(false)); f.Flags().Lookup("project") == nil || len(f.Commands()) != 1 {
		t.Error("doctor lacks --project or its fix subcommand")
	}
}
