package image

import (
	"context"
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/monoes/mono-agent/internal/fsconfine"
)

// maxFontBytes caps a custom font file read into memory.
const maxFontBytes = 32 << 20

var (
	defaultFontOnce sync.Once
	defaultFont     *opentype.Font
	defaultFontErr  error
)

// loadFont returns the embedded Go Regular font, or the TrueType/OpenType
// font at path (confined to the org workdir when the run has one).
func loadFont(ctx context.Context, path string) (*opentype.Font, error) {
	if path == "" {
		defaultFontOnce.Do(func() { defaultFont, defaultFontErr = opentype.Parse(goregular.TTF) })
		return defaultFont, defaultFontErr
	}
	p, err := fsconfine.Path(ctx, expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	if fi.Size() > maxFontBytes {
		return nil, fmt.Errorf("font: %q is over the %d byte limit", p, maxFontBytes)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("font: %q is not a TrueType/OpenType font: %w", p, err)
	}
	return f, nil
}

// wrapText breaks text into lines of at most lineLength characters at
// spaces; explicit newlines are kept and a longer word gets its own line.
func wrapText(text string, lineLength int) []string {
	var lines []string
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		cur := ""
		for _, word := range strings.Split(para, " ") {
			switch {
			case cur == "":
				cur = word
			case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) > lineLength:
				lines = append(lines, cur)
				cur = word
			default:
				cur += " " + word
			}
		}
		lines = append(lines, strings.TrimSpace(cur))
	}
	return lines
}

// drawText renders the wrapped text block in FontColor. The block is
// anchored by the alignments (west/center/east, north/middle/south) and
// offset by PositionX/PositionY from that anchor, like GraphicsMagick's
// gravity: from the left/top edge for west/north, from the centre for
// center/middle, and inwards from the right/bottom edge for east/south.
// Each line is aligned within the block the same way horizontally.
func drawText(ctx context.Context, img *image.NRGBA, op editOp) error {
	if strings.TrimSpace(op.Text) == "" {
		return nil
	}
	f, err := loadFont(ctx, op.Font)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: op.FontSize, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return fmt.Errorf("font: %w", err)
	}
	defer face.Close()

	lines := wrapText(op.Text, op.LineLength)
	m := face.Metrics()
	lineH := m.Height.Ceil()
	widths := make([]int, len(lines))
	blockW := 0
	for i, l := range lines {
		widths[i] = font.MeasureString(face, l).Ceil()
		blockW = max(blockW, widths[i])
	}
	blockH := lineH * len(lines)
	b := img.Bounds()

	x0 := b.Min.X + op.PositionX
	switch op.HAlign {
	case "center":
		x0 = b.Min.X + (b.Dx()-blockW)/2 + op.PositionX
	case "east", "right":
		x0 = b.Max.X - blockW - op.PositionX
	}
	y0 := b.Min.Y + op.PositionY
	switch op.VAlign {
	case "middle", "center":
		y0 = b.Min.Y + (b.Dy()-blockH)/2 + op.PositionY
	case "south", "bottom":
		y0 = b.Max.Y - blockH - op.PositionY
	}

	d := &font.Drawer{Dst: img, Src: image.NewUniform(op.FontColor), Face: face}
	for i, l := range lines {
		x := x0
		switch op.HAlign {
		case "center":
			x += (blockW - widths[i]) / 2
		case "east", "right":
			x += blockW - widths[i]
		}
		d.Dot = fixed.P(x, y0+i*lineH+m.Ascent.Ceil())
		d.DrawString(l)
	}
	return nil
}
