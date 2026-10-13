package image

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// blendOverAt paints c over the NRGBA pixel p (4 bytes) with source-over.
func blendOverAt(p []uint8, c color.NRGBA) {
	if c.A == 0 {
		return
	}
	if c.A == 255 {
		p[0], p[1], p[2], p[3] = c.R, c.G, c.B, 255
		return
	}
	sa, da := float64(c.A)/255, float64(p[3])/255
	oa := sa + da*(1-sa)
	mix := func(s, d uint8) uint8 {
		return uint8(math.Round((float64(s)*sa + float64(d)*da*(1-sa)) / oa))
	}
	p[0], p[1], p[2], p[3] = mix(c.R, p[0]), mix(c.G, p[1]), mix(c.B, p[2]), uint8(math.Round(oa*255))
}

func paint(img *image.NRGBA, x, y int, c color.NRGBA) {
	if !(image.Point{x, y}).In(img.Bounds()) {
		return
	}
	i := img.PixOffset(x, y)
	blendOverAt(img.Pix[i:i+4], c)
}

// drawShape draws a filled circle, filled (optionally rounded) rectangle or
// a one-pixel line in Color. Coordinates are inclusive pixel positions; for
// a circle Start is the centre and End a point on its edge.
func drawShape(img *image.NRGBA, op editOp) {
	switch op.Primitive {
	case "line":
		drawLine(img, op.StartX, op.StartY, op.EndX, op.EndY, op.Color)
	case "circle":
		r := math.Hypot(float64(op.EndX-op.StartX), float64(op.EndY-op.StartY))
		ri := int(math.Ceil(r))
		area := image.Rect(op.StartX-ri, op.StartY-ri, op.StartX+ri+1, op.StartY+ri+1).Intersect(img.Bounds())
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				if math.Hypot(float64(x-op.StartX), float64(y-op.StartY)) <= r {
					paint(img, x, y, op.Color)
				}
			}
		}
	default: // rectangle
		x0, x1 := minMax(op.StartX, op.EndX)
		y0, y1 := minMax(op.StartY, op.EndY)
		rad := op.CornerRadius
		if half := min((x1-x0)/2, (y1-y0)/2); rad > half {
			rad = half
		}
		area := image.Rect(x0, y0, x1+1, y1+1).Intersect(img.Bounds())
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				if rad > 0 {
					cx, cy := clampInt(x, x0+rad, x1-rad), clampInt(y, y0+rad, y1-rad)
					if (x-cx)*(x-cx)+(y-cy)*(y-cy) > rad*rad {
						continue
					}
				}
				paint(img, x, y, op.Color)
			}
		}
	}
}

func minMax(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

// drawLine draws a one-pixel Bresenham line, clipped to the image.
func drawLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	dx, dy := x1-x0, -(y1 - y0)
	if dx < 0 {
		dx = -dx
	}
	if dy > 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		paint(img, x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

// makeTransparent sets alpha to 0 on every pixel whose color is within Fuzz
// percent (RGB distance) of Color.
func makeTransparent(img *image.NRGBA, op editOp) {
	maxDist := op.Fuzz / 100 * math.Sqrt(3*255*255)
	for i := 0; i+3 < len(img.Pix); i += 4 {
		dr := float64(img.Pix[i]) - float64(op.Color.R)
		dg := float64(img.Pix[i+1]) - float64(op.Color.G)
		db := float64(img.Pix[i+2]) - float64(op.Color.B)
		if math.Sqrt(dr*dr+dg*dg+db*db) <= maxDist {
			img.Pix[i+3] = 0
		}
	}
}

// compositeImage lays src over dst at (PositionX, PositionY) with Operator.
// Porter-Duff operators (Over, In, Out, Atop, Xor) work on premultiplied
// color. Arithmetic operators combine straight RGB with a blend function
// and are then placed source-over (W3C compositing): Plus (sum, clamped),
// Add (sum, wrapping), Minus (overlay - image, clamped), Subtract (overlay -
// image, wrapping), Multiply, Difference, Divide (image / overlay), Bumpmap
// (image shaded by the overlay's intensity). Copy replaces pixels;
// CopyOpacity sets alpha from the overlay's alpha (or its intensity when it
// is opaque); CopyRed/Green/Blue copy one channel. Only the overlay's
// footprint changes. The CMYK channel operators are not supported on RGB.
func compositeImage(dst, src *image.NRGBA, op editOp) error {
	switch op.Operator {
	case "CopyBlack", "CopyCyan", "CopyMagenta", "CopyYellow":
		return fmt.Errorf("operator: %s needs a CMYK image; this node works on RGB(A) images (use CopyRed/CopyGreen/CopyBlue)", op.Operator)
	}
	opaqueSrc := true
	for i := 3; i < len(src.Pix); i += 4 {
		if src.Pix[i] != 255 {
			opaqueSrc = false
			break
		}
	}
	sb := src.Bounds()
	off := image.Pt(op.PositionX, op.PositionY)
	area := sb.Sub(sb.Min).Add(off).Intersect(dst.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			si := src.PixOffset(x-off.X+sb.Min.X, y-off.Y+sb.Min.Y)
			di := dst.PixOffset(x, y)
			composePixel(dst.Pix[di:di+4], src.Pix[si:si+4], op.Operator, opaqueSrc)
		}
	}
	return nil
}

func composePixel(d, s []uint8, operator string, opaqueSrc bool) {
	switch operator {
	case "Copy":
		copy(d, s)
		return
	case "CopyOpacity":
		if opaqueSrc {
			d[3] = uint8(math.Round(0.299*float64(s[0]) + 0.587*float64(s[1]) + 0.114*float64(s[2])))
		} else {
			d[3] = s[3]
		}
		return
	case "CopyRed":
		d[0] = s[0]
		return
	case "CopyGreen":
		d[1] = s[1]
		return
	case "CopyBlue":
		d[2] = s[2]
		return
	}
	sa, da := float64(s[3])/255, float64(d[3])/255
	var sc, dc [3]float64
	for k := 0; k < 3; k++ {
		sc[k], dc[k] = float64(s[k])/255, float64(d[k])/255
	}
	var fs, fd float64 // Porter-Duff weights of source and destination
	switch operator {
	case "Over":
		fs, fd = 1, 1-sa
	case "In":
		fs, fd = da, 0
	case "Out":
		fs, fd = 1-da, 0
	case "Atop":
		fs, fd = da, 1-sa
	case "Xor":
		fs, fd = 1-da, 1-sa
	default: // blend operators: mix the blend result into the source, then Over
		for k := 0; k < 3; k++ {
			sc[k] = (1-da)*sc[k] + da*blend(operator, dc[k], sc[k], s)
		}
		fs, fd = 1, 1-sa
	}
	oa := sa*fs + da*fd
	if oa <= 0 {
		d[0], d[1], d[2], d[3] = 0, 0, 0, 0
		return
	}
	for k := 0; k < 3; k++ {
		v := (sc[k]*sa*fs + dc[k]*da*fd) / oa
		d[k] = uint8(math.Round(clampF(v, 0, 1) * 255))
	}
	d[3] = uint8(math.Round(clampF(oa, 0, 1) * 255))
}

// blend combines backdrop channel b and source channel c (both 0..1).
func blend(operator string, b, c float64, s []uint8) float64 {
	wrap := func(v float64) float64 { return float64(int(math.Round(v*255))&0xFF) / 255 }
	switch operator {
	case "Plus":
		return math.Min(1, b+c)
	case "Add":
		return wrap(b + c)
	case "Minus":
		return math.Max(0, c-b)
	case "Subtract":
		return wrap(c - b)
	case "Multiply":
		return b * c
	case "Difference":
		return math.Abs(b - c)
	case "Divide":
		if c == 0 {
			return 1
		}
		return math.Min(1, b/c)
	case "Bumpmap":
		return b * (0.299*float64(s[0]) + 0.587*float64(s[1]) + 0.114*float64(s[2])) / 255
	}
	return c
}
