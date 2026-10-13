package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

// ---- helpers shared by the image.edit tests -------------------------------

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// binItem is an item carrying img as the "data" binary property.
func binItem(t *testing.T, img image.Image) workflow.Item {
	return workflow.Item{JSON: map[string]interface{}{
		"data":      base64.StdEncoding.EncodeToString(pngBytes(t, img)),
		"file_name": "photo.png",
		"mime_type": "image/png",
	}}
}

func runEdit(t *testing.T, config map[string]interface{}, items ...workflow.Item) []workflow.Item {
	t.Helper()
	out, err := runEditErr(config, items...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func runEditErr(config map[string]interface{}, items ...workflow.Item) ([]workflow.Item, error) {
	out, err := (&ImageEditNode{}).Execute(context.Background(), workflow.NodeInput{Items: items}, config)
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

// outImage decodes binary property prop of an output item.
func outImage(t *testing.T, item workflow.Item, prop string) *image.NRGBA {
	t.Helper()
	data, _, err := readBinaryProp(item, prop)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decodeImageBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return d.Img
}

func px(img *image.NRGBA, x, y int) color.NRGBA {
	return img.NRGBAAt(x, y)
}

func wantPx(t *testing.T, img *image.NRGBA, x, y int, want color.NRGBA) {
	t.Helper()
	if got := px(img, x, y); got != want {
		t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
	}
}

func wantSize(t *testing.T, img *image.NRGBA, w, h int) {
	t.Helper()
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
}

var (
	red     = color.NRGBA{255, 0, 0, 255}
	blue    = color.NRGBA{0, 0, 255, 255}
	white   = color.NRGBA{255, 255, 255, 255}
	black   = color.NRGBA{0, 0, 0, 255}
	green   = color.NRGBA{0, 255, 0, 255}
	noColor = color.NRGBA{}
)

// ---- input / output conventions --------------------------------------------

func TestEditBinaryRoundTripAndMetadata(t *testing.T) {
	item := binItem(t, solid(10, 10, red))
	item.JSON["keep"] = "me"
	out := runEdit(t, map[string]interface{}{"operation": "border", "border_width": 1, "border_height": 1}, item)
	j := out[0].JSON
	if j["keep"] != "me" || j["file_name"] != "photo.png" || j["mime_type"] != "image/png" || j["file_extension"] != "png" {
		t.Errorf("metadata = %v", j)
	}
	if n, _ := j["file_size"].(int); n == 0 {
		t.Errorf("file_size = %v", j["file_size"])
	}
	wantSize(t, outImage(t, out[0], "data"), 12, 12)
	if item.JSON["file_size"] != nil {
		t.Error("input item was modified")
	}
}

func TestEditItemBinaryMapInputAndDestinationKey(t *testing.T) {
	item := workflow.Item{JSON: map[string]interface{}{}, Binary: map[string][]byte{
		"img": pngBytes(t, solid(4, 4, red)), "other": []byte("x"),
	}}
	out := runEdit(t, map[string]interface{}{"operation": "crop", "width": 2, "height": 2,
		"data_property_name": "img", "destination_key": "small", "file_name": "small.png"}, item)
	wantSize(t, outImage(t, out[0], "small"), 2, 2)
	if out[0].JSON["file_name"] != "small.png" {
		t.Errorf("file_name = %v", out[0].JSON["file_name"])
	}
	if string(out[0].Binary["other"]) != "x" || out[0].Binary["img"] == nil {
		t.Errorf("Binary = %v", out[0].Binary)
	}
}

func TestEditFilesListShape(t *testing.T) {
	item := workflow.Item{JSON: map[string]interface{}{
		"pic":   base64.StdEncoding.EncodeToString(pngBytes(t, solid(3, 3, red))),
		"files": []interface{}{map[string]interface{}{"property": "pic", "file_name": "a.png", "mime_type": "image/png"}},
	}}
	out := runEdit(t, map[string]interface{}{"operation": "blur", "data_property_name": "pic"}, item)
	files := out[0].JSON["files"].([]interface{})
	if len(files) != 1 || files[0].(map[string]interface{})["file_name"] != "a.png" {
		t.Errorf("files = %v", files)
	}
	if _, flat := out[0].JSON["file_name"]; flat {
		t.Error("flat metadata written next to a files list")
	}
}

func TestEditFormatOption(t *testing.T) {
	out := runEdit(t, map[string]interface{}{"operation": "blur", "format": "jpeg", "quality": 80}, binItem(t, solid(8, 8, red)))
	j := out[0].JSON
	if j["mime_type"] != "image/jpeg" || j["file_name"] != "photo.jpeg" || j["file_extension"] != "jpeg" {
		t.Errorf("metadata = %v", j)
	}
	data, _, _ := readBinaryProp(out[0], "data")
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("output is not jpeg: %v", err)
	}

	out = runEdit(t, map[string]interface{}{"operation": "blur", "format": "webp"}, binItem(t, solid(8, 8, red)))
	if out[0].JSON["mime_type"] != "image/png" || out[0].JSON["image_edit_warning"] == nil {
		t.Errorf("webp fallback = %v", out[0].JSON)
	}
	for _, f := range []string{"gif", "bmp", "tiff"} {
		out = runEdit(t, map[string]interface{}{"operation": "blur", "format": f}, binItem(t, solid(8, 8, red)))
		if got := outImage(t, out[0], "data"); got.Bounds().Dx() != 8 {
			t.Errorf("%s: bad output", f)
		}
	}
}

func TestEditPathMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	if err := os.WriteFile(src, pngBytes(t, solid(10, 6, red)), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	item := workflow.Item{JSON: map[string]interface{}{"pic": src}}
	out := runEdit(t, map[string]interface{}{"operation": "rotate", "rotate": 90, "field": "pic", "output_dir": outDir}, item)
	p, _ := out[0].JSON["image_path"].(string)
	if !strings.HasPrefix(p, outDir) || out[0].JSON["image_width"] != 6 || out[0].JSON["image_height"] != 10 {
		t.Fatalf("path mode output = %v", out[0].JSON)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := decodeImageBytes(data); d.Img.Bounds().Dx() != 6 {
		t.Error("written file has wrong size")
	}

	// An item with only image_path is read from the path without "field".
	out = runEdit(t, map[string]interface{}{"operation": "crop", "width": 2, "height": 2, "file_name": "c.png"},
		workflow.Item{JSON: map[string]interface{}{"image_path": src}})
	if got := out[0].JSON["image_path"]; got != filepath.Join(dir, "c.png") {
		t.Errorf("image_path = %v", got)
	}

	// create writing a file needs output_dir.
	out = runEdit(t, map[string]interface{}{"operation": "create", "width": 3, "height": 3, "output_dir": outDir, "output_field": "made"},
		workflow.Item{JSON: map[string]interface{}{}})
	if _, err := os.Stat(out[0].JSON["made"].(string)); err != nil {
		t.Errorf("created file: %v", err)
	}
	if _, err := runEditErr(map[string]interface{}{"operation": "create", "field": "x"}, workflow.Item{JSON: map[string]interface{}{}}); err == nil || !strings.Contains(err.Error(), "output_dir") {
		t.Errorf("create without output_dir: %v", err)
	}
}

// ---- information -----------------------------------------------------------

func TestEditInformation(t *testing.T) {
	img := solid(30, 20, red)
	img.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 100})
	out := runEdit(t, map[string]interface{}{"operation": "information"}, binItem(t, img))
	j := out[0].JSON
	if j["format"] != "PNG" || j["width"] != 30 || j["height"] != 20 || j["geometry"] != "30x20" ||
		j["mime_type"] != "image/png" || j["has_alpha"] != true || j["depth"] != 8 || j["color_model"] == "" {
		t.Errorf("info = %v", j)
	}
	if size, _ := j["size"].(map[string]interface{}); size["width"] != 30 {
		t.Errorf("size = %v", j["size"])
	}
	if n, _ := j["file_size"].(int); n <= 0 || j["data"] == nil {
		t.Errorf("file_size = %v / data kept = %v", j["file_size"], j["data"] != nil)
	}
}

// jpegWithOrientation returns a JPEG of img carrying an EXIF orientation tag.
func jpegWithOrientation(t *testing.T, img image.Image, o uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(tiff[18:], o)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	b := buf.Bytes()
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, b[2:]...)
}

func TestEditInformationJPEGOrientation(t *testing.T) {
	data := jpegWithOrientation(t, solid(40, 20, red), 6)
	item := workflow.Item{JSON: map[string]interface{}{"data": base64.StdEncoding.EncodeToString(data)}}
	j := runEdit(t, map[string]interface{}{"operation": "information"}, item)[0].JSON
	if j["orientation"] != 6 || j["width"] != 20 || j["height"] != 40 || j["format"] != "JPEG" || j["color_model"] != "YCbCr" {
		t.Errorf("info = %v", j)
	}
}

// ---- multi_step ------------------------------------------------------------

func TestEditMultiStep(t *testing.T) {
	steps := []interface{}{
		map[string]interface{}{"operation": "resize", "width": 50, "height": 50, "resize_option": "ignoreAspectRatio"},
		map[string]interface{}{"operation": "border", "border_width": 5, "border_height": 5, "border_color": "#0000ff"},
		map[string]interface{}{"operation": "crop", "width": 10, "height": 10},
	}
	out := runEdit(t, map[string]interface{}{"operation": "multi_step", "operations": steps}, binItem(t, solid(20, 20, red)))
	img := outImage(t, out[0], "data")
	wantSize(t, img, 10, 10)
	wantPx(t, img, 0, 0, blue)
	wantPx(t, img, 9, 9, red)

	// JSON string, n8n fixedCollection shape and camelCase keys.
	js := `{"operations":[{"operation":"create","backgroundColor":"#00ff00","width":6,"height":4},
		{"operation":"border","borderWidth":1,"borderHeight":2,"borderColor":"#ff0000"}]}`
	out = runEdit(t, map[string]interface{}{"operation": "multi_step", "operations": js}, workflow.Item{JSON: map[string]interface{}{}})
	img = outImage(t, out[0], "data")
	wantSize(t, img, 8, 8)
	wantPx(t, img, 0, 0, red)
	wantPx(t, img, 1, 2, green)
	if out[0].JSON["file_name"] != "image.png" {
		t.Errorf("file_name = %v", out[0].JSON["file_name"])
	}
}

func TestEditMultiStepErrors(t *testing.T) {
	cases := map[string]interface{}{
		"operations[0].operation": []interface{}{map[string]interface{}{"operation": "information"}},
		"create can only be":      []interface{}{map[string]interface{}{"operation": "blur"}, map[string]interface{}{"operation": "create"}},
		"operations[1].border_color": []interface{}{map[string]interface{}{"operation": "blur"},
			map[string]interface{}{"operation": "border", "border_color": "nope"}},
		"at least one step": "[]",
		"not valid JSON":    "[{",
	}
	for want, ops := range cases {
		_, err := runEditErr(map[string]interface{}{"operation": "multi_step", "operations": ops}, binItem(t, solid(2, 2, red)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want %q", ops, err, want)
		}
	}
}

// ---- config errors and limits ----------------------------------------------

func TestEditConfigErrorsNameTheField(t *testing.T) {
	cases := []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"operation": "explode"}, "image.edit: operation:"},
		{map[string]interface{}{"operation": "border", "border_color": "nope"}, "image.edit: border_color:"},
		{map[string]interface{}{"operation": "border", "border_width": "wide"}, "image.edit: border_width:"},
		{map[string]interface{}{"operation": "resize", "resize_option": "huge"}, "image.edit: resize_option:"},
		{map[string]interface{}{"operation": "composite"}, "image.edit: data_property_name_composite:"},
		{map[string]interface{}{"operation": "blur", "format": "heic"}, "image.edit: format:"},
		{map[string]interface{}{"operation": "blur", "quality": 101}, "image.edit: quality:"},
		{map[string]interface{}{"operation": "shear", "degrees_x": 90}, "image.edit: degrees_x"},
		{map[string]interface{}{"operation": "transparent", "fuzz": 120}, "image.edit: fuzz:"},
		{map[string]interface{}{"operation": "blur", "hex_alpha": "x"}, "image.edit: hex_alpha:"},
	}
	for _, c := range cases {
		_, err := runEditErr(c.cfg, binItem(t, solid(2, 2, red)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want %q", c.cfg, err, c.want)
		}
	}
}

func TestEditMissingInput(t *testing.T) {
	_, err := runEditErr(map[string]interface{}{"operation": "blur"}, workflow.Item{JSON: map[string]interface{}{"x": 1}})
	if err == nil || !strings.Contains(err.Error(), `binary property "data"`) {
		t.Errorf("err = %v", err)
	}
	_, err = runEditErr(map[string]interface{}{"operation": "blur"}, workflow.Item{JSON: map[string]interface{}{"data": "!!!"}})
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Errorf("err = %v", err)
	}
}

func TestEditRejectsDecompressionBomb(t *testing.T) {
	data, err := os.ReadFile(writeBombPNG(t, 20000, 20000))
	if err != nil {
		t.Fatal(err)
	}
	item := workflow.Item{JSON: map[string]interface{}{"data": base64.StdEncoding.EncodeToString(data)}}
	if _, err := runEditErr(map[string]interface{}{"operation": "information"}, item); err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Errorf("err = %v", err)
	}
	if _, err := runEditErr(map[string]interface{}{"operation": "create", "width": 20000, "height": 20000}, workflow.Item{JSON: map[string]interface{}{}}); err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Errorf("create err = %v", err)
	}
}

func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		mode hexAlphaMode
		want color.NRGBA
	}{
		{"#f00", hexAlpha, red},
		{"#ff000080", hexAlpha, color.NRGBA{255, 0, 0, 128}},
		{"#ff000000", hexOpacity, red},
		{"#ffffffff", hexOpacity, color.NRGBA{255, 255, 255, 0}},
		{"rgba(0, 0, 255, 0.5)", hexAlpha, color.NRGBA{0, 0, 255, 128}},
		{"rgb(0,255,0)", hexAlpha, green},
		{"Transparent", hexAlpha, noColor},
		{"black", hexAlpha, black},
	}
	for _, c := range cases {
		got, err := parseColor(c.in, c.mode)
		if err != nil || got != c.want {
			t.Errorf("parseColor(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	if _, err := parseColor("#12345", hexAlpha); err == nil {
		t.Error("bad hex accepted")
	}
}

func TestN8nParamMapCoversSchema(t *testing.T) {
	ours := map[string]bool{}
	for _, v := range N8nEditImageParams {
		ours[v] = true
	}
	for _, k := range []string{"border_width", "resize_option", "data_property_name_composite", "destination_key", "format", "quality"} {
		if !ours[k] {
			t.Errorf("no n8n mapping to %s", k)
		}
	}
	if N8nEditImageOperations["multiStep"] != opMultiStep {
		t.Error("multiStep not mapped")
	}
}

func TestEditPathModeConfinedToOrgWorkdir(t *testing.T) {
	root, outside := confinedImageDirs(t)
	ctx := fsconfine.WithRoot(context.Background(), root)
	node := &ImageEditNode{}
	for _, p := range []string{filepath.Join(outside, "pic.png"), "../outside/pic.png", "escape/pic.png"} {
		_, err := node.Execute(ctx, itemWith(p), map[string]interface{}{"operation": "blur"})
		if !errors.Is(err, fsconfine.ErrOutsideWorkdir) {
			t.Errorf("input %q: err = %v, want ErrOutsideWorkdir", p, err)
		}
	}
	inside := itemWith(filepath.Join(root, "pic.png"))
	for name, cfg := range map[string]map[string]interface{}{
		"output_dir": {"operation": "blur", "output_dir": outside},
		"font":       {"operation": "text", "text": "x", "font": filepath.Join(outside, "f.ttf")},
	} {
		if _, err := node.Execute(ctx, inside, cfg); !errors.Is(err, fsconfine.ErrOutsideWorkdir) {
			t.Errorf("%s: err = %v, want ErrOutsideWorkdir", name, err)
		}
	}
	withOverlay := workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(map[string]interface{}{
		"image_path": filepath.Join(root, "pic.png"), "over": filepath.Join(outside, "pic.png")})}}
	_, err := node.Execute(ctx, withOverlay, map[string]interface{}{"operation": "composite", "data_property_name_composite": "over", "field": "image_path"})
	if !errors.Is(err, fsconfine.ErrOutsideWorkdir) {
		t.Errorf("composite overlay: err = %v, want ErrOutsideWorkdir", err)
	}
	out, err := node.Execute(ctx, inside, map[string]interface{}{"operation": "blur"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := out[0].Items[0].JSON["image_path"].(string); !strings.HasPrefix(p, root) {
		t.Errorf("output %q is not under the workdir", p)
	}
}
