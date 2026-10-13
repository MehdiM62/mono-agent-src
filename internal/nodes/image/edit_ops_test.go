package image

import (
	"encoding/base64"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// editOne runs a single operation on img (as the "data" binary property)
// and returns the decoded result.
func editOne(t *testing.T, img image.Image, cfg map[string]interface{}) *image.NRGBA {
	t.Helper()
	return outImage(t, runEdit(t, cfg, binItem(t, img))[0], "data")
}

func TestEditBlur(t *testing.T) {
	img := solid(20, 20, black)
	for y := 0; y < 20; y++ {
		for x := 10; x < 20; x++ {
			img.SetNRGBA(x, y, white)
		}
	}
	out := editOne(t, img, map[string]interface{}{"operation": "blur", "blur": 5, "sigma": 2})
	if c := px(out, 10, 10); c.R == 255 || c.R == 0 {
		t.Errorf("edge pixel not blurred: %v", c)
	}
	wantPx(t, out, 0, 10, black)
	same := editOne(t, img, map[string]interface{}{"operation": "blur", "sigma": 0})
	wantPx(t, same, 10, 10, white)
}

func TestEditBorder(t *testing.T) {
	out := editOne(t, solid(10, 10, red), map[string]interface{}{
		"operation": "border", "border_width": 3, "border_height": 2, "border_color": "#0000ff"})
	wantSize(t, out, 16, 14)
	wantPx(t, out, 0, 0, blue)
	wantPx(t, out, 2, 5, blue)
	wantPx(t, out, 3, 2, red)
	wantPx(t, out, 12, 11, red)
	wantPx(t, out, 15, 13, blue)
	// n8n default: 10px black.
	out = editOne(t, solid(4, 4, red), map[string]interface{}{"operation": "border"})
	wantSize(t, out, 24, 24)
	wantPx(t, out, 0, 0, black)
}

func TestEditCreate(t *testing.T) {
	out := runEdit(t, map[string]interface{}{"operation": "create", "width": 7, "height": 5, "background_color": "#00ff00"},
		workflow.Item{JSON: map[string]interface{}{}})
	img := outImage(t, out[0], "data")
	wantSize(t, img, 7, 5)
	wantPx(t, img, 6, 4, green)
	if out[0].JSON["mime_type"] != "image/png" || out[0].JSON["file_name"] != "image.png" {
		t.Errorf("metadata = %v", out[0].JSON)
	}
	// n8n defaults: 50x50 white.
	img = outImage(t, runEdit(t, map[string]interface{}{"operation": "create"}, workflow.Item{JSON: map[string]interface{}{}})[0], "data")
	wantSize(t, img, 50, 50)
	wantPx(t, img, 0, 0, white)
}

func TestEditCrop(t *testing.T) {
	img := solid(20, 20, white)
	img.SetNRGBA(5, 6, red)
	img.SetNRGBA(8, 8, blue)
	out := editOne(t, img, map[string]interface{}{"operation": "crop", "width": 4, "height": 3, "position_x": 5, "position_y": 6})
	wantSize(t, out, 4, 3)
	wantPx(t, out, 0, 0, red)
	wantPx(t, out, 3, 2, blue)
	// Clipped to the image.
	wantSize(t, editOne(t, img, map[string]interface{}{"operation": "crop", "width": 50, "height": 50, "position_x": 15, "position_y": 18}), 5, 2)
	if _, err := runEditErr(map[string]interface{}{"operation": "crop", "width": 2, "height": 2, "position_x": 30}, binItem(t, img)); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("err = %v", err)
	}
}

func TestEditDraw(t *testing.T) {
	base := solid(20, 20, white)
	rect := editOne(t, base, map[string]interface{}{"operation": "draw", "primitive": "rectangle", "color": "#ff0000",
		"start_position_x": 5, "start_position_y": 7, "end_position_x": 2, "end_position_y": 3})
	wantPx(t, rect, 2, 3, red)
	wantPx(t, rect, 5, 7, red)
	wantPx(t, rect, 1, 3, white)
	wantPx(t, rect, 6, 7, white)
	wantPx(t, rect, 2, 8, white)

	round := editOne(t, base, map[string]interface{}{"operation": "draw", "color": "#ff0000", "corner_radius": 4,
		"start_position_x": 0, "start_position_y": 0, "end_position_x": 10, "end_position_y": 10})
	wantPx(t, round, 0, 0, white)
	wantPx(t, round, 10, 10, white)
	wantPx(t, round, 5, 5, red)
	wantPx(t, round, 0, 5, red)
	wantPx(t, round, 5, 0, red)

	circle := editOne(t, base, map[string]interface{}{"operation": "draw", "primitive": "circle", "color": "blue",
		"start_position_x": 10, "start_position_y": 10, "end_position_x": 10, "end_position_y": 14})
	wantPx(t, circle, 10, 10, blue)
	wantPx(t, circle, 14, 10, blue)
	wantPx(t, circle, 10, 6, blue)
	wantPx(t, circle, 15, 10, white)
	wantPx(t, circle, 13, 13, white)

	line := editOne(t, base, map[string]interface{}{"operation": "draw", "primitive": "line", "color": "#000",
		"start_position_x": 0, "start_position_y": 0, "end_position_x": 9, "end_position_y": 9})
	for i := 0; i <= 9; i++ {
		wantPx(t, line, i, i, black)
	}
	wantPx(t, line, 1, 0, white)
	wantPx(t, line, 10, 10, white)

	// Semi-transparent color blends over the image; hex_alpha=opacity reads
	// #rrggbb00 as opaque (n8n's default draw color).
	half := editOne(t, base, map[string]interface{}{"operation": "draw", "color": "#ff000080",
		"start_position_x": 0, "start_position_y": 0, "end_position_x": 1, "end_position_y": 1})
	wantPx(t, half, 0, 0, color.NRGBA{255, 127, 127, 255})
	n8n := editOne(t, base, map[string]interface{}{"operation": "draw", "color": "#ff000000", "hex_alpha": "opacity"})
	wantPx(t, n8n, 50%20, 50%20, white) // default start is (50,50): outside this 20x20 image
	n8n = editOne(t, solid(300, 300, white), map[string]interface{}{"operation": "draw", "color": "#ff000000", "hex_alpha": "opacity"})
	wantPx(t, n8n, 50, 50, red)
	wantPx(t, n8n, 250, 250, red)
	wantPx(t, n8n, 251, 250, white)
}

func TestEditResizeOptions(t *testing.T) {
	src := solid(200, 100, red)
	cases := []struct {
		option        string
		width, height float64
		wantW, wantH  int
	}{
		{"ignoreAspectRatio", 50, 50, 50, 50},
		{"maximumArea", 100, 50, 100, 50}, // area 5000 = 200x100 scaled by 0.5
		{"minimumArea", 50, 50, 100, 50},  // cover 50x50
		{"onlyIfLarger", 100, 100, 100, 50},
		{"onlyIfLarger", 400, 400, 200, 100},
		{"onlyIfSmaller", 400, 400, 400, 200},
		{"onlyIfSmaller", 100, 100, 200, 100},
		{"percent", 50, 25, 100, 25},
		{"ignoreAspectRatio", 100, 0, 100, 50}, // missing side keeps the aspect ratio
	}
	for _, c := range cases {
		out := editOne(t, src, map[string]interface{}{"operation": "resize", "resize_option": c.option, "width": c.width, "height": c.height})
		if b := out.Bounds(); b.Dx() != c.wantW || b.Dy() != c.wantH {
			t.Errorf("%s %vx%v: got %dx%d, want %dx%d", c.option, c.width, c.height, b.Dx(), b.Dy(), c.wantW, c.wantH)
		}
	}
	_, err := runEditErr(map[string]interface{}{"operation": "resize", "resize_option": "percent", "width": 100000, "height": 100000}, binItem(t, src))
	if err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Errorf("huge resize err = %v", err)
	}
}

func TestEditRotate(t *testing.T) {
	img := solid(20, 10, white)
	img.SetNRGBA(0, 0, red)
	out := editOne(t, img, map[string]interface{}{"operation": "rotate", "rotate": 90})
	wantSize(t, out, 10, 20)
	wantPx(t, out, 9, 0, red) // clockwise: top-left goes to top-right

	out = editOne(t, img, map[string]interface{}{"operation": "rotate", "rotate": -90})
	wantPx(t, out, 0, 19, red) // counter-clockwise: top-left goes to bottom-left

	out = editOne(t, solid(20, 20, red), map[string]interface{}{"operation": "rotate", "rotate": 45})
	if b := out.Bounds(); b.Dx() < 28 || b.Dx() > 30 {
		t.Errorf("45 degree canvas = %dx%d", b.Dx(), b.Dy())
	}
	if c := px(out, 0, 0); c.A != 0 {
		t.Errorf("corner = %v, want transparent default background", c)
	}
	c := px(out, out.Bounds().Dx()/2, out.Bounds().Dy()/2)
	if c != red {
		t.Errorf("centre = %v", c)
	}
	out = editOne(t, solid(20, 20, red), map[string]interface{}{"operation": "rotate", "rotate": 30, "background_color": "#0000ff"})
	wantPx(t, out, 0, 0, blue)
}

func TestEditShear(t *testing.T) {
	out := editOne(t, solid(10, 10, red), map[string]interface{}{"operation": "shear", "degrees_x": 45})
	wantSize(t, out, 20, 10)
	wantPx(t, out, 1, 0, red)
	if c := px(out, 18, 0); c.A != 0 {
		t.Errorf("top-right = %v, want background", c)
	}
	wantPx(t, out, 18, 9, red)
	if c := px(out, 1, 9); c.A != 0 {
		t.Errorf("bottom-left = %v, want background", c)
	}
	out = editOne(t, solid(10, 10, red), map[string]interface{}{"operation": "shear", "degrees_y": 45, "background_color": "white"})
	wantSize(t, out, 10, 20)
	wantPx(t, out, 9, 0, white)
	wantPx(t, out, 0, 0, red)
}

func TestEditText(t *testing.T) {
	base := solid(200, 60, white)
	out := editOne(t, base, map[string]interface{}{"operation": "text", "text": "Hello", "font_size": 24, "font_color": "#000000",
		"horizontal_alignment": "west", "vertical_alignment": "north", "position_x": 5, "position_y": 5})
	dark, darkRight := 0, 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 200; x++ {
			if px(out, x, y).R < 128 {
				dark++
				if x > 120 {
					darkRight++
				}
			}
		}
	}
	if dark < 30 || darkRight != 0 {
		t.Errorf("text pixels = %d (right half %d)", dark, darkRight)
	}
	// east alignment puts the text at the right edge.
	out = editOne(t, base, map[string]interface{}{"operation": "text", "text": "Hi", "font_size": 24, "horizontal_alignment": "east", "position_x": 0, "position_y": 0})
	found := false
	for y := 0; y < 60 && !found; y++ {
		for x := 170; x < 200; x++ {
			if px(out, x, y).R < 128 {
				found = true
			}
		}
	}
	if !found {
		t.Error("east-aligned text not near the right edge")
	}
	if _, err := runEditErr(map[string]interface{}{"operation": "text", "text": "x", "font": "/nonexistent/font.ttf"}, binItem(t, base)); err == nil || !strings.Contains(err.Error(), "font") {
		t.Errorf("missing font err = %v", err)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("aa bb cc\nddddddd e", 5)
	want := []string{"aa bb", "cc", "ddddddd", "e"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrapText = %q, want %q", got, want)
	}
}

func TestEditTransparent(t *testing.T) {
	img := solid(3, 1, red)
	img.SetNRGBA(1, 0, color.NRGBA{250, 0, 0, 255})
	img.SetNRGBA(2, 0, blue)
	out := editOne(t, img, map[string]interface{}{"operation": "transparent", "color": "#ff0000"})
	if px(out, 0, 0).A != 0 || px(out, 1, 0).A != 255 || px(out, 2, 0).A != 255 {
		t.Errorf("fuzz 0: %v %v %v", px(out, 0, 0), px(out, 1, 0), px(out, 2, 0))
	}
	out = editOne(t, img, map[string]interface{}{"operation": "transparent", "color": "#ff0000", "fuzz": 5})
	if px(out, 0, 0).A != 0 || px(out, 1, 0).A != 0 || px(out, 2, 0).A != 255 {
		t.Errorf("fuzz 5: %v %v %v", px(out, 0, 0), px(out, 1, 0), px(out, 2, 0))
	}
}

func TestEditCompositeOverPosition(t *testing.T) {
	item := binItem(t, solid(10, 10, white))
	item.JSON["overlay"] = base64.StdEncoding.EncodeToString(pngBytes(t, solid(4, 4, red)))
	out := outImage(t, runEdit(t, map[string]interface{}{"operation": "composite", "data_property_name_composite": "overlay",
		"position_x": 2, "position_y": 3}, item)[0], "data")
	wantPx(t, out, 2, 3, red)
	wantPx(t, out, 5, 6, red)
	wantPx(t, out, 1, 3, white)
	wantPx(t, out, 6, 6, white)
	wantPx(t, out, 2, 7, white)

	// Semi-transparent overlay, partly off the canvas (negative position).
	item.JSON["overlay"] = base64.StdEncoding.EncodeToString(pngBytes(t, solid(4, 4, color.NRGBA{255, 0, 0, 128})))
	out = outImage(t, runEdit(t, map[string]interface{}{"operation": "composite", "data_property_name_composite": "overlay",
		"position_x": -2, "position_y": -2}, item)[0], "data")
	wantPx(t, out, 0, 0, color.NRGBA{255, 127, 127, 255})
	wantPx(t, out, 2, 2, white)
}

func TestEditCompositeOperators(t *testing.T) {
	D := color.NRGBA{100, 150, 200, 255}
	S := color.NRGBA{200, 100, 50, 255}
	dst := solid(2, 1, D)
	dst.SetNRGBA(1, 0, noColor) // pixel 1 is fully transparent
	cases := []struct {
		op      string
		p0, p1  color.NRGBA
		checkP1 bool
	}{
		{"Over", S, S, true},
		{"In", S, noColor, true},
		{"Out", noColor, S, true},
		{"Atop", S, noColor, true},
		{"Xor", noColor, S, true},
		{"Copy", S, S, true},
		{"Multiply", color.NRGBA{78, 59, 39, 255}, S, true},
		{"Difference", color.NRGBA{100, 50, 150, 255}, S, true},
		{"Plus", color.NRGBA{255, 250, 250, 255}, S, true},
		{"Add", color.NRGBA{44, 250, 250, 255}, S, true},
		{"Minus", color.NRGBA{100, 0, 0, 255}, S, true},
		{"Subtract", color.NRGBA{100, 206, 106, 255}, S, true},
		{"Divide", color.NRGBA{128, 255, 255, 255}, S, true},
		{"Bumpmap", color.NRGBA{49, 73, 97, 255}, S, true},
		{"CopyRed", color.NRGBA{200, 150, 200, 255}, noColor, false},
		{"CopyGreen", color.NRGBA{100, 100, 200, 255}, noColor, false},
		{"CopyBlue", color.NRGBA{100, 150, 50, 255}, noColor, false},
		{"CopyOpacity", color.NRGBA{100, 150, 200, 124}, noColor, false},
	}
	for _, c := range cases {
		item := binItem(t, dst)
		item.JSON["over"] = base64.StdEncoding.EncodeToString(pngBytes(t, solid(2, 1, S)))
		out := outImage(t, runEdit(t, map[string]interface{}{"operation": "composite", "data_property_name_composite": "over", "operator": c.op}, item)[0], "data")
		if got := px(out, 0, 0); !nearly(got, c.p0) {
			t.Errorf("%s: opaque pixel = %v, want %v", c.op, got, c.p0)
		}
		if got := px(out, 1, 0); c.checkP1 && !nearly(got, c.p1) {
			t.Errorf("%s: transparent pixel = %v, want %v", c.op, got, c.p1)
		}
	}
	for _, op := range []string{"CopyBlack", "CopyCyan", "CopyMagenta", "CopyYellow"} {
		item := binItem(t, dst)
		item.JSON["over"] = item.JSON["data"]
		_, err := runEditErr(map[string]interface{}{"operation": "composite", "data_property_name_composite": "over", "operator": op}, item)
		if err == nil || !strings.Contains(err.Error(), "CMYK") {
			t.Errorf("%s: err = %v", op, err)
		}
	}
	_, err := runEditErr(map[string]interface{}{"operation": "composite", "data_property_name_composite": "missing"}, binItem(t, dst))
	if err == nil || !strings.Contains(err.Error(), "data_property_name_composite") {
		t.Errorf("missing overlay err = %v", err)
	}
}

// nearly compares colors allowing 1 unit of rounding per channel; fully
// transparent colors compare equal regardless of RGB.
func nearly(a, b color.NRGBA) bool {
	if a.A == 0 && b.A == 0 {
		return true
	}
	d := func(x, y uint8) bool { return x-y <= 1 || y-x <= 1 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}
