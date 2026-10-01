package infill

import (
	"image/color"
	"math"
	"path/filepath"
	"testing"

	"github.com/lockyc/imgkit/internal/raster"
)

// dropped runs edgeStops and returns which known pixels it cut off, as a
// frame-sized lookup.
func dropped(t *testing.T, holes string, w, h int) func(x, y int) bool {
	t.Helper()
	pieces, err := edgeStops(holes, t.TempDir(), 3, 150)
	if err != nil {
		t.Fatal(err)
	}
	cut := make([]bool, w*h)
	for _, p := range pieces {
		img, err := raster.Load(p.path)
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if img.RGBA64At(x, y).A > 0 {
					cut[(p.at.Y+y)*w+p.at.X+x] = true
				}
			}
		}
	}
	return func(x, y int) bool { return cut[y*w+x] }
}

func frac(w, h int, keep, of func(x, y int) bool) float64 {
	var k, n float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if of(x, y) {
				n++
				if keep(x, y) {
					k++
				}
			}
		}
	}
	return k / n
}

// A dark band 30 px below the hole is cut off from it; the grain on the
// hole's own side is not.
func TestEdgeStopsCutOffABandAcrossAnEdge(t *testing.T) {
	const w, h, band = 400, 400, 230
	in := disc(200, 160, 40)
	holes, _ := ground(t, t.TempDir(), w, h, func(x, y int) (int, int) {
		if y >= band {
			return 30, 10
		}
		return 160, 20
	}, in)
	cut := dropped(t, holes, w, h)
	if f := frac(w, h, cut, func(x, y int) bool { return y >= band+5 }); f < 0.99 {
		t.Errorf("%.3f of the band is cut off, want all of it", f)
	}
	if f := frac(w, h, cut, func(x, y int) bool { return y < band-5 && !in(x, y) }); f > 0.1 {
		t.Errorf("%.3f of the hole's own ground is cut off, want under 0.1", f)
	}
}

// A hole across the edge touches both grounds, so it keeps both.
func TestEdgeStopsKeepBothSidesOfAStraddledEdge(t *testing.T) {
	const w, h, band = 400, 400, 160
	in := disc(200, 160, 40)
	holes, _ := ground(t, t.TempDir(), w, h, func(x, y int) (int, int) {
		if y >= band {
			return 30, 10
		}
		return 160, 20
	}, in)
	cut := dropped(t, holes, w, h)
	for name, side := range map[string]func(x, y int) bool{
		"above": func(x, y int) bool { return y < band-5 && !in(x, y) },
		"below": func(x, y int) bool { return y >= band+5 && !in(x, y) },
	} {
		if f := frac(w, h, cut, side); f > 0.1 {
			t.Errorf("%.3f of the ground %s the edge is cut off, want under 0.1", f, name)
		}
	}
}

func TestEdgeStopsNoneOnOneGround(t *testing.T) {
	holes, _ := ground(t, t.TempDir(), 300, 300, func(x, y int) (int, int) { return 128, 0 }, disc(150, 150, 30))
	pieces, err := edgeStops(holes, t.TempDir(), 3, 150)
	if err != nil || pieces != nil {
		t.Errorf("pieces %v, err %v; want none on a flat ground", pieces, err)
	}
}

// holed loads an image and makes a disc of it transparent, as infill's
// holes image.
func holed(t *testing.T, path string, cx, cy, r int) string {
	t.Helper()
	src, err := raster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	in := disc(cx, cy, r)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if in(x, y) {
				src.SetRGBA64(x, y, color.RGBA64{})
			}
		}
	}
	p := filepath.Join(t.TempDir(), "holes.png")
	if err := raster.SavePNG(p, src); err != nil {
		t.Fatal(err)
	}
	return p
}

// Plaster's speckles and paper's fibres rise above the gradient limit, and
// fibres enclose darker pockets, but none of it is another ground, so a hole
// in either with no edge near it loses none of its ground.
func TestEdgeStopsNoneOnGrainyGrounds(t *testing.T) {
	for _, c := range []struct {
		asset  string
		cx, cy int
	}{{"flat-ground.jpg", 760, 760}, {"paper-ground.jpg", 800, 800}} {
		holes := holed(t, filepath.Join("..", "..", "quality", "assets", c.asset), c.cx, c.cy, 150)
		pieces, err := edgeStops(holes, t.TempDir(), 3, 150)
		if err != nil || pieces != nil {
			t.Errorf("%s: pieces %v, err %v; want none with no edge", c.asset, pieces, err)
		}
	}
}

// A thin line 30 px below the hole is a strong edge, but the ground beyond
// it is the same ground, so nothing is cut off.
func TestEdgeStopsKeepTheSameGroundBeyondALine(t *testing.T) {
	const w, h, line = 400, 400, 230
	holes, _ := ground(t, t.TempDir(), w, h, func(x, y int) (int, int) {
		if y >= line && y < line+3 {
			return 20, 0
		}
		return 160, 20
	}, disc(200, 160, 40))
	pieces, err := edgeStops(holes, t.TempDir(), 3, 150)
	if err != nil || pieces != nil {
		t.Errorf("pieces %v, err %v; want none beyond a line in one ground", pieces, err)
	}
}

// A dark rail 30 px below the hole, with the hole's own ground below it, is
// cut off; the ground beyond it is not.
func TestEdgeStopsCutOffARailNotTheGroundBeyond(t *testing.T) {
	const w, h, top, bottom = 400, 400, 230, 270
	in := disc(200, 160, 40)
	holes, _ := ground(t, t.TempDir(), w, h, func(x, y int) (int, int) {
		if y >= top && y < bottom {
			return 30, 10
		}
		return 160, 20
	}, in)
	cut := dropped(t, holes, w, h)
	if f := frac(w, h, cut, func(x, y int) bool { return y >= top+5 && y < bottom-5 }); f < 0.99 {
		t.Errorf("%.3f of the rail is cut off, want all of it", f)
	}
	for name, side := range map[string]func(x, y int) bool{
		"above": func(x, y int) bool { return y < top-5 && !in(x, y) },
		"below": func(x, y int) bool { return y >= bottom+5 },
	} {
		if f := frac(w, h, cut, side); f > 0.1 {
			t.Errorf("%.3f of the ground %s the rail is cut off, want under 0.1", f, name)
		}
	}
}

// A one-pixel speck in the mask near a hole in paper does not make the
// paper's fibre pockets count as another ground.
func TestEdgeStopsIgnoreAStraySpeck(t *testing.T) {
	src, err := raster.Load(holed(t, filepath.Join("..", "..", "quality", "assets", "paper-ground.jpg"), 800, 800, 150))
	if err != nil {
		t.Fatal(err)
	}
	src.SetRGBA64(1150, 800, color.RGBA64{})
	holes := filepath.Join(t.TempDir(), "holes.png")
	if err := raster.SavePNG(holes, src); err != nil {
		t.Fatal(err)
	}
	pieces, err := edgeStops(holes, t.TempDir(), 3, 150)
	if err != nil || pieces != nil {
		t.Errorf("pieces %v, err %v; want none with no edge", pieces, err)
	}
}

// A rail is judged by what it would draw into the hole, not by how much of
// the crop it fills: specks down a tall frame, which stretch the crop to
// six times the bare hole's, leave the rail cut off. (The crop reaches 450
// px either side of the hole, so the rail is judged within that.)
func TestEdgeStopsJudgeARailTheSameWhateverTheCrop(t *testing.T) {
	const w, h, top, bottom = 1000, 4000, 230, 270
	at := func(x, y int) (int, int) {
		if y >= top && y < bottom {
			return 30, 10
		}
		return 160, 20
	}
	in := disc(500, 160, 40)
	specks := func(x, y int) bool { return in(x, y) || (x == 500 && y >= 900 && (y-900)%800 == 0) }
	for name, hole := range map[string]func(x, y int) bool{"bare": in, "specks": specks} {
		holes, _ := ground(t, t.TempDir(), w, h, at, hole)
		cut := dropped(t, holes, w, h)
		if f := frac(w, h, cut, func(x, y int) bool { return x >= 100 && x < 900 && y >= top+5 && y < bottom-5 }); f < 0.99 {
			t.Errorf("%s: %.3f of the rail is cut off, want all of it", name, f)
		}
		if f := frac(w, h, cut, func(x, y int) bool { return (y < top-5 || y >= bottom+5) && !hole(x, y) }); f > 0.01 {
			t.Errorf("%s: %.3f of the ground is cut off, want under 0.01", name, f)
		}
	}
}

// A dark block below a hole in a 1000 x 1000 frame is cut off when the
// coarsest level would draw at least a level of its tone into the hole:
// small and near, or just over that; not just under it, and not a larger
// one far off. Its pull, in levels of tone, is noted on each.
func TestEdgeStopsCutOffABlockByItsPullOnTheHole(t *testing.T) {
	const hy, hr = 300, 40
	for _, c := range []struct {
		name      string
		wide, gap int // the block's side (or width, for the slab) and its gap below the hole
		tall      int
		cut       bool
	}{
		{"small and near (1.5)", 60, 30, 60, true},
		{"just over (1.14)", 54, 30, 54, true},
		{"just under (0.81)", 48, 30, 48, false},
		{"larger and far (0.43)", 300, 350, 100, false},
	} {
		top := hy + hr + c.gap
		block := func(x, y int) bool {
			return y >= top && y < top+c.tall && x >= 500-c.wide/2 && x < 500-c.wide/2+c.wide
		}
		in := disc(500, hy, hr)
		holes, _ := ground(t, t.TempDir(), 1000, 1000, func(x, y int) (int, int) {
			if block(x, y) {
				return 30, 10
			}
			return 160, 20
		}, in)
		cut := dropped(t, holes, 1000, 1000)
		inner := func(x, y int) bool {
			return y >= top+10 && y < top+c.tall-10 && x >= 510-c.wide/2 && x < 490+c.wide-c.wide/2
		}
		want := 0.0
		if c.cut {
			want = 1
		}
		if f := frac(1000, 1000, cut, inner); math.Abs(f-want) > 0.01 {
			t.Errorf("%s: %.3f of the block is cut off, want %.0f", c.name, f, want)
		}
		if f := frac(1000, 1000, cut, func(x, y int) bool { return !in(x, y) && !block(x, y) && (y < top-10 || y >= top+c.tall+10) }); f > 0.001 {
			t.Errorf("%s: %.4f of the ground is cut off, want none", c.name, f)
		}
	}
}
