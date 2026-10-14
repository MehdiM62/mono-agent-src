package bridge

import (
	"strings"
	"testing"

	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/extension"
)

// narrowed is a bridge narrowed to a profile.
type narrowed struct{ profile string }

func (f *narrowed) IsConnected() bool                    { return true }
func (f *narrowed) CreateTab(string) (int, error)        { return 1, nil }
func (f *narrowed) CloseTab(int) error                   { return nil }
func (f *narrowed) NewPage(int) browserpkg.PageInterface { return nil }

// router narrows to a profile.
type router struct{ narrowed }

func (r *router) ForProfile(p string) browserpkg.ExtensionBridge { return &narrowed{profile: p} }

func TestForProfile(t *testing.T) {
	t.Parallel()
	r := &router{}
	if got := ForProfile(r, "p-work"); got.(*narrowed).profile != "p-work" {
		t.Fatalf("router not narrowed: %+v", got)
	}
	if got := ForProfile(r, ""); got != browserpkg.ExtensionBridge(r) {
		t.Fatal("an empty profile must leave the bridge alone")
	}
	p := &narrowed{}
	if got := ForProfile(p, "p-work"); got != browserpkg.ExtensionBridge(p) {
		t.Fatal("a bridge that cannot route is returned as is")
	}
}

type routed struct {
	narrowed
	err error
}

func (r *routed) Route() (extension.ConnInfo, error) { return extension.ConnInfo{}, r.err }

func TestRouteHintNamesTheFix(t *testing.T) {
	t.Parallel()
	stub := &routed{err: &extension.NoBrowserError{Profile: "p-x", BoundTo: []string{"p-a"}}}
	if h := RouteHint(stub); !strings.Contains(h, `"p-x"`) || !strings.Contains(h, "extension bind") {
		t.Fatalf("hint = %q", h)
	}
	if h := RouteHint(&routed{}); h != "" {
		t.Fatalf("a route that works needs no hint, got %q", h)
	}
	if h := RouteHint(&narrowed{}); h != "" {
		t.Fatalf("no hint for a plain bridge, got %q", h)
	}
}
