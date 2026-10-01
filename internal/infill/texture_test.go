package infill

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/raster"
)

// ground writes a w x h ground whose grey level and grain amplitude come from
// at, transparent inside the holes, plus a flat opaque fill of grey 128: the
// holes and fill images infill hands to texture.
func ground(t *testing.T, dir string, w, h int, at func(x, y int) (grey, amp int), hole func(x, y int) bool) (holes, fill string) {
	rng := rand.New(rand.NewPCG(7, 7))
	hi := image.NewNRGBA(image.Rect(0, 0, w, h))
	fi := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			g, a := at(x, y)
			v := uint8(g + rng.IntN(2*a+1) - a)
			if !hole(x, y) {
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
	return holes, fill
}

func disc(cx, cy, r int) func(x, y int) bool {
	return func(x, y int) bool { return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r }
}

// grained runs texture and lays its pieces over the fill, as infill does.
func grained(t *testing.T, holes, fill string, strength float64, seed int) *image.RGBA64 {
	t.Helper()
	pieces, err := texture(holes, fill, t.TempDir(), 2, strength, seed)
	if err != nil {
		t.Fatal(err)
	}
	img, err := raster.Load(fill)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pieces {
		pi, err := raster.Load(p.path)
		if err != nil {
			t.Fatal(err)
		}
		draw.Draw(img, pi.Bounds().Add(p.at), pi, image.Point{}, draw.Over)
	}
	return img
}

// stats is the mean and std-dev of the green channel where keep holds.
func stats(img *image.RGBA64, keep func(x, y int) bool) (mean, std float64) {
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
	return s / n, math.Sqrt(ss/n - (s/n)*(s/n))
}

func load(t *testing.T, path string) *image.RGBA64 {
	img, err := raster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestTextureMatchesTheGround(t *testing.T) {
	dir := t.TempDir()
	in := disc(100, 80, 40)
	holes, fill := ground(t, dir, 200, 160, func(x, y int) (int, int) { return 128, 20 }, in)
	_, ground := stats(load(t, holes), func(x, y int) bool { return !in(x, y) })
	_, full := stats(grained(t, holes, fill, 1, 1), in)
	if r := full / ground; r < 0.8 || r > 1.25 {
		t.Errorf("hole std / ground std = %.3f, want 0.8 to 1.25", r)
	}
	if _, half := stats(grained(t, holes, fill, 0.5, 1), in); math.Abs(half/full-0.5) > 0.05 {
		t.Errorf("strength 0.5 gives %.3f of strength 1, want 0.5", half/full)
	}
}

// Two holes in grounds of different grain each take their own ground's, not
// the other's. They share one crop, and the grains differ by less than the
// energy cap, so only sampling around each hole keeps them apart.
func TestTextureSamplesEachHole(t *testing.T) {
	dir := t.TempDir()
	left, right := disc(150, 150, 30), disc(730, 150, 30)
	at := func(x, y int) (int, int) {
		if x < 440 {
			return 128, 20
		}
		return 128, 11
	}
	holes, fill := ground(t, dir, 1200, 300, at, func(x, y int) bool { return left(x, y) || right(x, y) })
	src, out := load(t, holes), grained(t, holes, fill, 1, 1)
	for name, c := range map[string]struct {
		hole   func(x, y int) bool
		ground func(x, y int) bool
	}{
		"left":  {left, func(x, y int) bool { return x < 400 && !left(x, y) }},
		"right": {right, func(x, y int) bool { return x >= 480 && !right(x, y) }},
	} {
		_, g := stats(src, c.ground)
		_, f := stats(out, c.hole)
		if r := f / g; r < 0.85 || r > 1.18 {
			t.Errorf("%s hole std / its ground's std = %.3f, want 0.85 to 1.18", name, r)
		}
	}
}

// A dark band within reach of the hole's patches lends it neither its
// darkness nor the streak along its edge.
func TestTextureRejectsEdges(t *testing.T) {
	dir := t.TempDir()
	in := disc(350, 250, 30)
	band := 250 + 30 + maxPatch // one patch-width below the hole, inside the 2-patch source reach
	at := func(x, y int) (int, int) {
		if y >= band {
			return 20, 10
		}
		return 160, 20
	}
	holes, fill := ground(t, dir, 700, 700, at, in)
	out := grained(t, holes, fill, 1, 1)
	if mean, _ := stats(out, in); math.Abs(mean-128.0/255) > 2.0/255 {
		t.Errorf("hole mean %.1f, want the fill's 128 ± 2", mean*255)
	}
	var worst float64
	for y := 0; y < 700; y++ {
		for x := 0; x < 700; x++ {
			if in(x, y) {
				worst = math.Max(worst, math.Abs(float64(out.RGBA64At(x, y).G)/0xffff-128.0/255))
			}
		}
	}
	if worst > 45.0/255 {
		t.Errorf("hole strays %.0f levels from the fill, want at most 45 (grain ±20)", worst*255)
	}
}

func TestTextureIsSeeded(t *testing.T) {
	dir := t.TempDir()
	in := disc(100, 80, 40)
	holes, fill := ground(t, dir, 200, 160, func(x, y int) (int, int) { return 128, 20 }, in)
	read := func(seed int) []byte {
		pieces, err := texture(holes, fill, t.TempDir(), 2, 1, seed)
		if err != nil || len(pieces) != 1 {
			t.Fatalf("pieces %v, err %v", pieces, err)
		}
		b, _ := os.ReadFile(pieces[0].path)
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
	_, err := texture(holes, holes, dir, 2, 1, 1)
	if err == nil || !strings.Contains(err.Error(), "--grain 0") {
		t.Errorf("err = %v, want a pointer to --grain 0", err)
	}
}
