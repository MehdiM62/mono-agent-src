package profilegate

import (
	"testing"

	"github.com/spf13/cobra"
)

func tree() *cobra.Command {
	root := &cobra.Command{Use: "monoagentcli"}
	people := &cobra.Command{Use: "people"}
	people.AddCommand(&cobra.Command{Use: "list"})
	cfg := &cobra.Command{Use: "config"}
	cfg.AddCommand(&cobra.Command{Use: "get"})
	library := &cobra.Command{Use: "library"}
	library.AddCommand(&cobra.Command{Use: "status"}, &cobra.Command{Use: "install"})
	auto := &cobra.Command{Use: "automation"}
	auto.AddCommand(&cobra.Command{Use: "list"}, &cobra.Command{Use: "install"})
	root.AddCommand(people, cfg, library, auto, &cobra.Command{Use: "brand-new"})
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
