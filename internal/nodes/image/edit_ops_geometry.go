package image

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/disintegration/imaging"
)

// checkCanvas rejects an output size that is empty or over maxImagePixels,
// so an upscale, border or create cannot allocate an unbounded buffer.
func checkCanvas(w, h int) error {
	if w < 1 || h < 1 {
		return fmt.Errorf("result would be %dx%d pixels, which is empty", w, h)
	}
	if px := int64(w) * int64(h); px > maxImagePixels {
		return fmt.Errorf("result would be %dx%d pixels (%d), over the %d pixel limit", w, h, px, maxImagePixels)
	}
	return nil
}

func createCanvas(op editOp) (*image.NRGBA, error) {
	w, h := int(math.Round(op.Width)), int(math.Round(op.Height))
	if err := checkCanvas(w, h); err != nil {
		return nil, err
	}
	return imaging.New(w, h, op.Background), nil
}

// blurImage applies a Gaussian blur. The kernel spans 3 sigma each side; the
// blur (radius) parameter is accepted for n8n compatibility but the kernel
// size follows sigma, and sigma 0 leaves the image unchanged.
func blurImage(img *image.NRGBA, op editOp) *image.NRGBA {
	if op.Sigma <= 0 {
		return img
	}
	return imaging.Blur(img, op.Sigma)
}

// borderImage adds BorderWidth pixels left and right and BorderHeight
// pixels top and bottom, filled with BorderColor.
func borderImage(img *image.NRGBA, op editOp) (*image.NRGBA, error) {
	b := img.Bounds()
	w, h := b.Dx()+2*op.BorderWidth, b.Dy()+2*op.BorderHeight
	if err := checkCanvas(w, h); err != nil {
		return nil, err
	}
	out := imaging.New(w, h, op.BorderColor)
	draw.Draw(out, image.Rect(op.BorderWidth, op.BorderHeight, op.BorderWidth+b.Dx(), op.BorderHeight+b.Dy()), img, b.Min, draw.Src)
	return out, nil
}

// cropImage cuts the Width x Height region at (PositionX, PositionY),
// clipped to the image.
func cropImage(img *image.NRGBA, op editOp) (*image.NRGBA, error) {
	x, y := op.PositionX, op.PositionY
	r := image.Rect(x, y, x+int(math.Round(op.Width)), y+int(math.Round(op.Height))).Intersect(img.Bounds())
	if r.Empty() {
		return nil, fmt.Errorf("crop: region %vx%v at (%d,%d) lies outside the %dx%d image", op.Width, op.Height, x, y, img.Bounds().Dx(), img.Bounds().Dy())
	}
	return imaging.Crop(img, r), nil
}

// resizeTarget computes the new size for a resize option, following the
// GraphicsMagick geometry flags n8n's options stand for. ok is false when
// the option says to leave the image as it is.
func resizeTarget(w, h int, op editOp) (nw, nh int, ok bool) {
	fw, fh := float64(w), float64(h)
	W, H := op.Width, op.Height
	scaleFit := func(pickMin bool) float64 { // uses only the dimensions that are set
		sx, sy := W/fw, H/fh
		switch {
		case W == 0:
			return sy
		case H == 0:
			return sx
		case pickMin:
			return math.Min(sx, sy)
		}
		return math.Max(sx, sy)
	}
	scaled := func(s float64) (int, int, bool) {
		return roundDim(fw * s), roundDim(fh * s), true
	}
	switch op.ResizeOption {
	case "ignoreAspectRatio":
		if W == 0 {
			W = fw * H / fh
		}
		if H == 0 {
			H = fh * W / fw
		}
		return roundDim(W), roundDim(H), true
	case "percent":
		if W == 0 {
			W = H
		}
		if H == 0 {
			H = W
		}
		return roundDim(fw * W / 100), roundDim(fh * H / 100), true
	case "maximumArea": // W*H is the pixel area to resize to, aspect kept
		area := W * H
		if W == 0 || H == 0 {
			area = math.Max(W, H) * math.Max(W, H)
		}
		return scaled(math.Sqrt(area / (fw * fh)))
	case "minimumArea": // cover: both sides at least the given size
		return scaled(scaleFit(false))
	case "onlyIfLarger": // shrink to fit, never enlarge
		if s := scaleFit(true); s < 1 {
			return scaled(s)
		}
	case "onlyIfSmaller": // enlarge to fit, never shrink
		if s := scaleFit(true); s > 1 {
			return scaled(s)
		}
	}
	return w, h, false
}

func roundDim(v float64) int {
	if v < 1 {
		return 1
	}
	if v > 1<<30 {
		return 1 << 30
	}
	return int(math.Round(v))
}

func resizeImage(img *image.NRGBA, op editOp) (*image.NRGBA, error) {
	b := img.Bounds()
	nw, nh, ok := resizeTarget(b.Dx(), b.Dy(), op)
	if !ok || (nw == b.Dx() && nh == b.Dy()) {
		return img, nil
	}
	if err := checkCanvas(nw, nh); err != nil {
		return nil, err
	}
	return imaging.Resize(img, nw, nh, imaging.Lanczos), nil
}

// rotateImage turns the image clockwise by Rotate degrees; the canvas grows
// to hold the rotated image and exposed corners get Background.
func rotateImage(img *image.NRGBA, op editOp) (*image.NRGBA, error) {
	deg := math.Mod(op.Rotate, 360)
	if deg == 0 {
		return img, nil
	}
	b := img.Bounds()
	rad := deg * math.Pi / 180
	sin, cos := math.Abs(math.Sin(rad)), math.Abs(math.Cos(rad))
	if err := checkCanvas(int(math.Ceil(float64(b.Dx())*cos+float64(b.Dy())*sin)), int(math.Ceil(float64(b.Dx())*sin+float64(b.Dy())*cos))); err != nil {
		return nil, err
	}
	return imaging.Rotate(img, -deg, op.Background), nil // imaging turns counter-clockwise
}

// shearImage shears along X by DegreesX then along Y by DegreesY, growing
// the canvas to fit; exposed areas get Background.
func shearImage(img *image.NRGBA, op editOp) (*image.NRGBA, error) {
	tx, ty := math.Tan(op.DegreesX*math.Pi/180), math.Tan(op.DegreesY*math.Pi/180)
	if tx == 0 && ty == 0 {
		return img, nil
	}
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	// Forward map: x' = x + tx*y ; y' = y + ty*x' = ty*x + (1+tx*ty)*y
	fwd := func(x, y float64) (float64, float64) { return x + tx*y, ty*x + (1+tx*ty)*y }
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
		x, y := fwd(c[0], c[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	ow, oh := int(math.Ceil(maxX-minX)), int(math.Ceil(maxY-minY))
	if err := checkCanvas(ow, oh); err != nil {
		return nil, err
	}
	out := imaging.New(ow, oh, op.Background)
	src := imaging.Clone(img) // zero-based bounds
	for oy := 0; oy < oh; oy++ {
		for ox := 0; ox < ow; ox++ {
			// Inverse map of the pixel centre (determinant is 1).
			X, Y := float64(ox)+0.5+minX, float64(oy)+0.5+minY
			y := Y - ty*X
			x := X - tx*y
			if x < 0 || y < 0 || x >= w || y >= h {
				continue
			}
			c := sampleBilinear(src, x-0.5, y-0.5)
			i := out.PixOffset(ox, oy)
			blendOverAt(out.Pix[i:i+4], c)
		}
	}
	return out, nil
}

// sampleBilinear samples src at (x, y) in pixel coordinates, interpolating
// premultiplied so transparent neighbours do not bleed their color.
func sampleBilinear(src *image.NRGBA, x, y float64) color.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var r, g, bl, a float64
	for _, s := range [4]struct {
		dx, dy int
		wt     float64
	}{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
		px, py := clampInt(x0+s.dx, 0, w-1), clampInt(y0+s.dy, 0, h-1)
		p := src.Pix[src.PixOffset(px, py):]
		pa := float64(p[3]) / 255 * s.wt
		r += float64(p[0]) * pa
		g += float64(p[1]) * pa
		bl += float64(p[2]) * pa
		a += pa
	}
	if a <= 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{uint8(math.Round(r / a)), uint8(math.Round(g / a)), uint8(math.Round(bl / a)), uint8(math.Round(a * 255))}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
