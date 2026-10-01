package infill

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/raster"
)

// grainyHole writes a grey ground with per-pixel grain and a transparent
// disc (the holes image infill makes), plus a flat opaque fill of the same
// grey, and returns their paths and the disc's test.
func grainyHole(t *testing.T, dir string) (holes, fill string, in func(x, y int) bool) {
	const w, h, cx, cy, r = 200, 160, 100, 80, 40
	in = func(x, y int) bool { return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r }
	rng := rand.New(rand.NewPCG(7, 7))
	hi := image.NewNRGBA(image.Rect(0, 0, w, h))
	fi := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(128 + rng.IntN(41) - 20)
			if !in(x, y) {
				hi.Set(x, y, color.NRGBA{v, v, v, 255})
			}
			fi.Set(x, y, color.NRGBA{128, 128, 128, 255})
		}
	}
	holes, fill = filepath.Join(dir, "holes.png"), filepath.Join(dir, "fill.png")
	if err := raster.SavePNG(holes, hi); err != nil {
		t.Fatal(err)
	}
	if err := raster.SavePNG(fill, fi); err != nil {
		t.Fatal(err)
	}
	return holes, fill, in
}

func lumaStd(t *testing.T, path string, keep func(x, y int) bool) float64 {
	img, err := raster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var s, ss, n float64
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if keep(x, y) {
				l := float64(img.RGBA64At(x, y).G) / 0xffff
				s, ss, n = s+l, ss+l*l, n+1
			}
		}
	}
	return math.Sqrt(ss/n - (s/n)*(s/n))
}

func TestTextureMatchesTheGround(t *testing.T) {
	dir := t.TempDir()
	holes, fill, in := grainyHole(t, dir)
	out := filepath.Join(dir, "out.png")
	if err := texture(holes, fill, out, 2, 1, 1); err != nil {
		t.Fatal(err)
	}
	ground := lumaStd(t, holes, func(x, y int) bool { return !in(x, y) })
	ratio := lumaStd(t, out, in) / ground
	if ratio < 0.8 || ratio > 1.25 {
		t.Errorf("hole std / ground std = %.3f, want 0.8 to 1.25", ratio)
	}
	half := filepath.Join(dir, "half.png")
	if err := texture(holes, fill, half, 2, 0.5, 1); err != nil {
		t.Fatal(err)
	}
	if r := lumaStd(t, half, in) / ground; math.Abs(r-ratio/2) > 0.05 {
		t.Errorf("strength 0.5 gives ratio %.3f, want about %.3f", r, ratio/2)
	}
}

func TestTextureIsSeeded(t *testing.T) {
	dir := t.TempDir()
	holes, fill, _ := grainyHole(t, dir)
	read := func(seed int) []byte {
		out := filepath.Join(dir, "out.png")
		if err := texture(holes, fill, out, 2, 1, seed); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		return b
	}
	if a, b := read(1), read(1); !bytes.Equal(a, b) {
		t.Error("one seed gave two results")
	}
	if a, b := read(1), read(2); bytes.Equal(a, b) {
		t.Error("two seeds gave one result")
	}
}

func TestTextureNeedsGround(t *testing.T) {
	dir := t.TempDir()
	holes := filepath.Join(dir, "holes.png")
	raster.SavePNG(holes, image.NewNRGBA(image.Rect(0, 0, 20, 20)))
	err := texture(holes, holes, filepath.Join(dir, "out.png"), 2, 1, 1)
	if err == nil || !strings.Contains(err.Error(), "--grain 0") {
		t.Errorf("err = %v, want a pointer to --grain 0", err)
	}
}
