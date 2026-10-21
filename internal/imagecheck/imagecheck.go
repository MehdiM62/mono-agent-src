// Package imagecheck reads an image's own header before anything is made of it:
// the size it claims, and whether it is animated. A few bytes can claim a
// picture that takes hundreds of megabytes to decode.
package imagecheck

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg" // registers the decoder for DecodeConfig
	_ "image/png"  // registers the decoder for DecodeConfig

	_ "golang.org/x/image/webp" // registers the decoder for DecodeConfig
)

const (
	// MaxSide is the longest side, in pixels, a picture may claim.
	MaxSide = 8192
	// MaxPixels is width times height a picture may claim (25 megapixels).
	MaxPixels = 25_000_000
)

// Check refuses a picture whose header cannot be read, that claims more than
// MaxSide or MaxPixels, or that is animated (a GIF with several frames, a WebP
// with the animation flag).
func Check(data []byte) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("its size cannot be read: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return errors.New("it claims no size")
	}
	if cfg.Width > MaxSide || cfg.Height > MaxSide || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return fmt.Errorf("it claims %dx%d pixels; the limit is %d a side and %d megapixels", cfg.Width, cfg.Height, MaxSide, MaxPixels/1_000_000)
	}
	switch format {
	case "gif":
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("it cannot be read: %w", err)
		}
		if len(g.Image) > 1 {
			return errors.New("animated pictures are not taken")
		}
	case "webp":
		// RIFF....WEBPVP8X<size>flags: bit 1 is the animation flag.
		if len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0 {
			return errors.New("animated pictures are not taken")
		}
	}
	return nil
}
