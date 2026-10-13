package image

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register the WebP decoder with image.Decode

	"github.com/monoes/mono-agent/internal/workflow"
)

// Binary (file) data on items, the convention the data.* nodes use: a
// "binary property" is an item JSON field holding the file as base64 (default
// "data"), with file_name, mime_type, file_extension and file_size next to
// it — or, for several files, a "files" list of {property, file_name, ...}.
// Bytes in workflow.Item.Binary[prop] and an n8n-shaped object
// {data, fileName, mimeType} are accepted on input too.

// maxEditInputBytes caps one encoded image read into memory.
const maxEditInputBytes = 256 << 20

const defaultBinaryProp = "data"

type fileMeta struct {
	FileName, MimeType, Extension string
}

// hasBinary reports whether item carries binary property prop.
func hasBinary(item workflow.Item, prop string) bool {
	if _, ok := item.Binary[prop]; ok {
		return true
	}
	switch item.JSON[prop].(type) {
	case string, map[string]interface{}, []byte:
		return true
	}
	return false
}

// readBinaryProp returns the bytes and metadata of binary property prop.
func readBinaryProp(item workflow.Item, prop string) ([]byte, fileMeta, error) {
	meta := lookupFileMeta(item.JSON, prop)
	var data []byte
	if b, ok := item.Binary[prop]; ok {
		data = b
	} else {
		switch v := item.JSON[prop].(type) {
		case string:
			b, err := decodeBase64Any(v)
			if err != nil {
				return nil, meta, fmt.Errorf("binary property %q is not base64: %w", prop, err)
			}
			data = b
		case map[string]interface{}:
			s, _ := v["data"].(string)
			b, err := decodeBase64Any(s)
			if err != nil {
				return nil, meta, fmt.Errorf("binary property %q: data is not base64: %w", prop, err)
			}
			data = b
			if s, _ := v["fileName"].(string); s != "" {
				meta.FileName = s
			}
			if s, _ := v["mimeType"].(string); s != "" {
				meta.MimeType = s
			}
			if s, _ := v["fileExtension"].(string); s != "" {
				meta.Extension = s
			}
		case []byte:
			data = v
		case nil:
			return nil, meta, fmt.Errorf("binary property %q not found on the item", prop)
		default:
			return nil, meta, fmt.Errorf("binary property %q holds %T, want a base64 string", prop, v)
		}
	}
	if len(data) > maxEditInputBytes {
		return nil, meta, fmt.Errorf("binary property %q is %d bytes, over the %d byte limit", prop, len(data), maxEditInputBytes)
	}
	return data, meta, nil
}

func decodeBase64Any(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ","); i >= 0 {
			s = s[i+1:]
		}
	}
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	var lastErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b, err := enc.DecodeString(s)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func lookupFileMeta(j map[string]interface{}, prop string) fileMeta {
	if files, ok := j["files"].([]interface{}); ok {
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok && m["property"] == prop {
				return fileMeta{asString(m["file_name"]), asString(m["mime_type"]), asString(m["file_extension"])}
			}
		}
	}
	return fileMeta{asString(j["file_name"]), asString(j["mime_type"]), asString(j["file_extension"])}
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// writeBinaryProp stores data as binary property prop on j: in the "files"
// list when the item uses that shape, else as flat metadata keys.
func writeBinaryProp(j map[string]interface{}, prop string, data []byte, meta fileMeta) {
	j[prop] = base64.StdEncoding.EncodeToString(data)
	entry := map[string]interface{}{
		"file_name":      meta.FileName,
		"mime_type":      meta.MimeType,
		"file_extension": meta.Extension,
		"file_size":      len(data),
	}
	if files, ok := j["files"].([]interface{}); ok {
		out := make([]interface{}, 0, len(files)+1)
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok && m["property"] == prop {
				continue
			}
			out = append(out, f)
		}
		entry["property"] = prop
		j["files"] = append(out, entry)
		return
	}
	for k, v := range entry {
		j[k] = v
	}
}

// readImageFile reads an image file with the same size cap as binary input.
func readImageFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEditInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEditInputBytes {
		return nil, fmt.Errorf("%q is over the %d byte limit", path, maxEditInputBytes)
	}
	return data, nil
}

// decodedImage is an image decoded from bytes, upright per its EXIF
// orientation, plus what image.edit's information operation reports.
type decodedImage struct {
	Img         *image.NRGBA
	Format      string // image.Decode's name: png, jpeg, gif, bmp, tiff, webp
	ColorModel  string
	Depth       int
	Orientation int // EXIF orientation (1-8), 0 when absent
	PaletteSize int
}

// decodeImageBytes decodes data after checking the declared pixel count
// against maxImagePixels (the decompression-bomb guard openImageSafely uses).
func decodeImageBytes(data []byte) (*decodedImage, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a supported image (png, jpeg, gif, bmp, tiff, webp): %w", err)
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > maxImagePixels {
		return nil, fmt.Errorf("image declares %dx%d pixels (%d), over the %d pixel limit", cfg.Width, cfg.Height, px, maxImagePixels)
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", format, err)
	}
	d := &decodedImage{Format: format, Depth: 8}
	switch s := src.(type) {
	case *image.NRGBA:
		d.ColorModel = "NRGBA"
	case *image.RGBA:
		d.ColorModel = "RGBA"
	case *image.NRGBA64:
		d.ColorModel, d.Depth = "NRGBA64", 16
	case *image.RGBA64:
		d.ColorModel, d.Depth = "RGBA64", 16
	case *image.Gray:
		d.ColorModel = "Gray"
	case *image.Gray16:
		d.ColorModel, d.Depth = "Gray16", 16
	case *image.YCbCr:
		d.ColorModel = "YCbCr"
	case *image.NYCbCrA:
		d.ColorModel = "NYCbCrA"
	case *image.CMYK:
		d.ColorModel = "CMYK"
	case *image.Paletted:
		d.ColorModel, d.PaletteSize = "Paletted", len(s.Palette)
	default:
		d.ColorModel = fmt.Sprintf("%T", src)
	}
	if format == "jpeg" {
		d.Orientation = jpegOrientation(data)
	}
	d.Img = orient(imaging.Clone(src), d.Orientation)
	return d, nil
}

// orient turns an image stored with EXIF orientation o upright.
func orient(img *image.NRGBA, o int) *image.NRGBA {
	switch o {
	case 2:
		return imaging.FlipH(img)
	case 3:
		return imaging.Rotate180(img)
	case 4:
		return imaging.FlipV(img)
	case 5:
		return imaging.Transpose(img)
	case 6:
		return imaging.Rotate270(img)
	case 7:
		return imaging.Transverse(img)
	case 8:
		return imaging.Rotate90(img)
	}
	return img
}

// jpegOrientation returns the EXIF orientation tag of a JPEG, 0 if none.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 0
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			return 0
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 0
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 0
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// outputFormat resolves the format to write: the requested one, else the
// input's. WebP has no pure-Go encoder, so it is written as PNG and the
// returned note says so.
func outputFormat(requested, input string) (imaging.Format, string, string, error) {
	f := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(requested), "."))
	if f == "" {
		f = input
	}
	switch f {
	case "", "png":
		return imaging.PNG, "png", "", nil
	case "jpg", "jpeg":
		return imaging.JPEG, "jpeg", "", nil
	case "gif":
		return imaging.GIF, "gif", "", nil
	case "tif", "tiff":
		return imaging.TIFF, "tiff", "", nil
	case "bmp":
		return imaging.BMP, "bmp", "", nil
	case "webp":
		return imaging.PNG, "png", "webp output is not available in this build; the image was written as png", nil
	}
	return 0, "", "", fmt.Errorf("format: %q is not one of bmp, gif, jpeg, png, tiff, webp", requested)
}

func encodeImage(img image.Image, f imaging.Format, quality int) ([]byte, error) {
	var buf bytes.Buffer
	var opts []imaging.EncodeOption
	if f == imaging.JPEG && quality > 0 {
		opts = append(opts, imaging.JPEGQuality(quality))
	}
	if err := imaging.Encode(&buf, img, f, opts...); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mimeForExt returns the image MIME type for a file extension.
func mimeForExt(ext string) string {
	if t := mime.TypeByExtension("." + ext); t != "" {
		return strings.Split(t, ";")[0]
	}
	return "image/" + ext
}

// swapExt replaces name's extension with ext (adding one if it had none).
func swapExt(name, ext string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + "." + ext
}
