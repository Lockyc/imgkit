// Package raster is imgkit's in-process pixel work: loading, measuring and
// small PNG edits that would cost a full re-encode through an engine.
// Images are premultiplied 16-bit RGBA, so colour comparisons read a
// transparent pixel as black.
package raster

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
)

// Load decodes a PNG or JPEG.
func Load(path string) (*image.RGBA64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	b := src.Bounds()
	dst := image.NewRGBA64(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst, nil
}

// SavePNG writes img as PNG.
func SavePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Frac is a rectangle as fractions of an image's width and height, so one
// region reads the same at any resolution.
type Frac struct{ X, Y, W, H float64 }

// ParseFrac reads "x,y,w,h", each between 0 and 1, with the box inside the image.
func ParseFrac(s string) (Frac, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return Frac{}, fmt.Errorf("region %q: want x,y,w,h as fractions of the image", s)
	}
	var v [4]float64
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || n < 0 || n > 1 {
			return Frac{}, fmt.Errorf("region %q: %q is not a fraction between 0 and 1", s, p)
		}
		v[i] = n
	}
	f := Frac{v[0], v[1], v[2], v[3]}
	if f.X+f.W > 1+1e-9 || f.Y+f.H > 1+1e-9 || f.W == 0 || f.H == 0 {
		return Frac{}, fmt.Errorf("region %q is empty or leaves the image", s)
	}
	return f, nil
}

// In converts f to pixels within b.
func (f Frac) In(b image.Rectangle) image.Rectangle {
	w, h := float64(b.Dx()), float64(b.Dy())
	return image.Rect(
		b.Min.X+int(math.Round(f.X*w)), b.Min.Y+int(math.Round(f.Y*h)),
		b.Min.X+int(math.Round((f.X+f.W)*w)), b.Min.Y+int(math.Round((f.Y+f.H)*h)),
	).Intersect(b)
}

// Channels selects what RMSE compares.
type Channels int

const (
	RGB   Channels = iota // premultiplied colour
	Alpha                 // coverage only
)

// RMSE is the root-mean-square difference over r, normalised to 0..1, the
// figure `magick compare -metric RMSE` prints in parentheses.
func RMSE(a, b *image.RGBA64, r image.Rectangle, ch Channels) (float64, error) {
	if a.Bounds() != b.Bounds() {
		return 0, fmt.Errorf("sizes differ: %dx%d vs %dx%d", a.Bounds().Dx(), a.Bounds().Dy(), b.Bounds().Dx(), b.Bounds().Dy())
	}
	r = r.Intersect(a.Bounds())
	if r.Empty() {
		return 0, fmt.Errorf("empty region")
	}
	var sum float64
	var n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pa, pb := a.RGBA64At(x, y), b.RGBA64At(x, y)
			if ch == Alpha {
				d := (float64(pa.A) - float64(pb.A)) / 65535
				sum += d * d
				n++
				continue
			}
			for _, d := range [3]float64{
				float64(pa.R) - float64(pb.R), float64(pa.G) - float64(pb.G), float64(pa.B) - float64(pb.B),
			} {
				d /= 65535
				sum += d * d
				n++
			}
		}
	}
	return math.Sqrt(sum / float64(n)), nil
}
