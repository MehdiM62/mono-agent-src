package profilegate

import (
	"testing"

	"github.com/spf13/cobra"
)

func leaf(name string) *cobra.Command {
	return &cobra.Command{Use: name, Run: func(*cobra.Command, []string) {}}
}

func tree() *cobra.Command {
	root := leaf("monoagentcli")
	people := leaf("people")
	people.AddCommand(leaf("list"))
	cfg := leaf("config")
	cfg.AddCommand(leaf("get"))
	library := leaf("library")
	library.AddCommand(leaf("status"), leaf("install"))
	auto := leaf("automation")
	auto.AddCommand(leaf("list"), leaf("install"))
	root.AddCommand(people, cfg, library, auto, leaf("brand-new"))
	return root
}

func find(root *cobra.Command, path ...string) *cobra.Command {
	c, _, _ := root.Find(path)
	return c
}

func TestClassFollowsNearestListedAncestor(t *testing.T) {
	root := tree()
	if got := Class(find(root, "people", "list")); got != ClassScoped {
		t.Fatalf("people list = %q, want scoped", got)
	}
	if got := Class(find(root, "config", "get")); got != ClassGlobal {
		t.Fatalf("config get = %q, want global", got)
	}
}

func TestAnUnlistedCommandIsScoped(t *testing.T) {
	if got := Class(find(tree(), "brand-new")); got != ClassScoped {
		t.Fatalf("unlisted command = %q, want scoped (fail closed)", got)
	}
}

func TestAScopedEntryOverridesAGlobalParent(t *testing.T) {
	root := tree()
	if got := Class(find(root, "automation", "list")); got != ClassScoped {
		t.Fatalf("automation list = %q, want scoped", got)
	}
	if got := Class(find(root, "automation", "install")); got != ClassGlobal {
		t.Fatalf("automation install = %q, want global", got)
	}
	if got := Class(find(root, "library", "status")); got != ClassGlobal {
		t.Fatalf("library status = %q, want global", got)
	}
	if got := Class(find(root, "library", "install")); got != ClassScoped {
		t.Fatalf("library install = %q, want scoped", got)
	}
}
