package stylebundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/extension"
)

// replayFile is a recording of TestLive against testdata/card.html.
const replayFile = "testdata/card.replay.json"

// liveSelectors are the fixture's elements the live run reads the source of.
var liveSelectors = []string{"#card", "#react-btn", "#vue-el", "#svelte-el", "#plain-el",
	"#react19-btn", "#nav-link", "#vue-insp-el", "#sentry-el"}

// TestLive builds a bundle in a real browser through the extension bridge.
// It runs only when STYLEBUNDLE_LIVE_BRIDGE is the bridge's base URL (a
// private one, e.g. http://127.0.0.1:9243, with HOME set to its scratch home
// so the pairing token matches) and STYLEBUNDLE_LIVE_PAGE the URL of
// testdata/card.html served over http. STYLEBUNDLE_RECORD=1 rewrites
// testdata/card.replay.json for the replay tests; STYLEBUNDLE_LIVE_OUT keeps
// the bundle folder for a look.
func TestLive(t *testing.T) {
	base, page := os.Getenv("STYLEBUNDLE_LIVE_BRIDGE"), os.Getenv("STYLEBUNDLE_LIVE_PAGE")
	if base == "" || page == "" {
		t.Skip("set STYLEBUNDLE_LIVE_BRIDGE and STYLEBUNDLE_LIVE_PAGE to run against a browser")
	}
	rs := extension.NewRemoteSender(base)
	tab, err := rs.CreateTab(page)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.CloseTab(tab)
	ep := extension.NewExtensionPage(rs, tab)
	if err := ep.WaitLoad(); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{Page: ep}

	b, err := Build(rec, "#card", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkCardBundle(t, b)
	for _, sel := range liveSelectors[1:] {
		if _, err := ReadSource(rec, sel); err != nil {
			t.Fatalf("source %s: %v", sel, err)
		}
	}
	if _, err := Build(rec, "#nothing-here", Options{}); err != ErrNotFound {
		t.Fatalf("missing element: %v", err)
	}
	if _, err := ReadSource(rec, "#nothing-here"); err != ErrNotFound {
		t.Fatalf("source of a missing element: %v", err)
	}
	if _, err := Build(rec, "div[", Options{}); err == nil {
		t.Fatal("bad selector accepted")
	}

	if out := os.Getenv("STYLEBUNDLE_LIVE_OUT"); out != "" {
		if _, err := b.WriteDir(out); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STYLEBUNDLE_RECORD") == "1" {
		if err := rec.save(filepath.FromSlash(replayFile)); err != nil {
			t.Fatal(err)
		}
	}
}
