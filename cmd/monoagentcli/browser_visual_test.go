package main

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/extension"
)

func writeDiffTestPNG(t *testing.T, path string, w, h int, dot bool) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	if dot {
		img.Set(1, 1, color.Black)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserDiff(t *testing.T) {
	bt := newBrowserTest(t, true)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	writeDiffTestPNG(t, a, 4, 4, false)
	writeDiffTestPNG(t, b, 4, 4, true)
	out := filepath.Join(dir, "d.png")

	res := bt.runJSON("diff", a, b, "--out", out, "--threshold", "0")
	if res["changed"] != float64(1) || res["ratio"] != 1.0/16 || res["path"] != out || res["sizeMismatch"] != false {
		t.Fatalf("diff = %v", res)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	if len(bt.fake.Calls()) != 0 {
		t.Fatalf("diff talked to the browser: %v", bt.fake.Types())
	}

	// Default path: under ./.monoagent/screenshots.
	wd := t.TempDir()
	t.Chdir(wd)
	res = bt.runJSON("diff", a, a)
	if res["changed"] != float64(0) || !strings.HasPrefix(res["path"].(string), filepath.Join(wd, ".monoagent", "screenshots")) {
		t.Fatalf("diff without --out = %v", res)
	}

	c := filepath.Join(dir, "c.png")
	writeDiffTestPNG(t, c, 6, 4, false)
	if res = bt.runJSON("diff", a, c); res["sizeMismatch"] != true || res["changed"] != float64(8) {
		t.Fatalf("size mismatch = %v", res)
	}

	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		exit int
	}{
		{[]string{"diff", a, filepath.Join(dir, "missing.png")}, 2},
		{[]string{"diff", a, bad}, 3},
		{[]string{"diff", a, b, "--threshold", "300"}, 3},
		{[]string{"diff", a}, 1},
	} {
		if _, err := bt.run(tc.args...); exitCodeFor(err) != tc.exit {
			t.Errorf("%v: exit %d, want %d: %v", tc.args, exitCodeFor(err), tc.exit, err)
		}
	}
}

func TestBrowserSweep(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", true)
	out := filepath.Join(t.TempDir(), "sweep")

	res := bt.runJSON("sweep", "--widths", "375, 1440px", "--media", "light,dark", "--out", out, "--element", "main")
	shots := res["shots"].([]any)
	if res["dir"] != out || len(shots) != 4 {
		t.Fatalf("sweep = %v", res)
	}
	want := []string{"375-light.png", "375-dark.png", "1440-light.png", "1440-dark.png"}
	for i, s := range shots {
		m := s.(map[string]any)
		if filepath.Base(m["path"].(string)) != want[i] || m["imageWidth"] != float64(375) {
			t.Errorf("shot %d = %v", i, m)
		}
		if _, err := os.Stat(m["path"].(string)); err != nil {
			t.Error(err)
		}
	}

	var emulations []map[string]any
	for _, c := range bt.fake.Calls() {
		switch c.Type {
		case "emulate":
			emulations = append(emulations, c.Params)
		case "screenshot":
			if c.Params["mode"] != "element" || c.Params["selector"] != "main" {
				t.Errorf("screenshot params %v", c.Params)
			}
		}
	}
	if len(emulations) != 5 || emulations[4]["reset"] != true {
		t.Fatalf("emulations %v", emulations)
	}
	first := emulations[0]
	if first["width"] != 375 || first["height"] != 900 || first["media"].(map[string]interface{})["prefersColorScheme"] != "light" {
		t.Fatalf("first emulation %v", first)
	}
}

func TestBrowserSweepResetsAfterAFailure(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", true)
	bt.fake.On("screenshot", func(*extension.Command) (any, error) { return nil, errors.New("capture failed") })
	_, err := bt.run("sweep", "--out", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "375px light: extension error: capture failed") {
		t.Fatalf("sweep with a failing capture: %v", err)
	}
	types := bt.fake.Types()
	if types[len(types)-1] != "emulate" || bt.fake.Calls()[len(types)-1].Params["reset"] != true {
		t.Fatalf("emulation not reset after the failure: %v", types)
	}
}

func TestBrowserSweepFlags(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", true)
	for _, args := range [][]string{
		{"sweep", "--widths", "wide"},
		{"sweep", "--widths", "0"},
		{"sweep", "--widths", ","},
		{"sweep", "--media", "blue"},
		{"sweep", "--height", "0"},
		{"sweep", "--viewport", "--element", "main"},
	} {
		if _, err := bt.run(args...); exitCodeFor(err) != 3 {
			t.Errorf("%v: exit %d: %v", args, exitCodeFor(err), err)
		}
	}
	if n := len(bt.fake.Calls()); n != 0 {
		t.Errorf("bad flags reached the browser: %v", bt.fake.Types())
	}
}
