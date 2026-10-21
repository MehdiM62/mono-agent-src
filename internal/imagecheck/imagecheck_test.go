package imagecheck

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngHeaderClaiming(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 1, 0 // 1 bit, grayscale
	binary.Write(&b, binary.BigEndian, uint32(13))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestCheck(t *testing.T) {
	small := image.NewGray(image.Rect(0, 0, 4, 4))
	var okPNG, okJPG, oneGIF, twoGIF bytes.Buffer
	png.Encode(&okPNG, small)
	jpeg.Encode(&okJPG, small, nil)
	pal := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	gif.Encode(&oneGIF, pal, nil)
	gif.EncodeAll(&twoGIF, &gif.GIF{Image: []*image.Paletted{pal, pal}, Delay: []int{1, 1}})

	cases := []struct {
		name string
		data []byte
		ok   bool
	}{
		{"png", okPNG.Bytes(), true},
		{"jpeg", okJPG.Bytes(), true},
		{"gif one frame", oneGIF.Bytes(), true},
		{"gif animated", twoGIF.Bytes(), false},
		{"60000x60000 png header", pngHeaderClaiming(60000, 60000), false},
		{"9000x10 png header", pngHeaderClaiming(9000, 10), false},
		{"8192x8192 png header (over 25 MP)", pngHeaderClaiming(8192, 8192), false},
		{"5000x5000 png header", pngHeaderClaiming(5000, 5000), true},
		{"magic only", []byte("\x89PNG\r\n\x1a\n"), false},
		{"unreadable webp", []byte("RIFF\x04\x00\x00\x00WEBPVP8 "), false},
		{"animated webp flag", append([]byte("RIFF\x20\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x09\x00\x00\x09\x00\x00"), make([]byte, 8)...), false},
	}
	for _, c := range cases {
		if err := Check(c.data); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
