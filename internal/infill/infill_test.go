package infill

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
	"github.com/lockyc/imgkit/internal/raster"
)

func pngOf(t *testing.T, dir, name string, w, h int) string {
	p := filepath.Join(dir, name)
	raster.SavePNG(p, image.NewRGBA(image.Rect(0, 0, w, h)))
	return p
}

// holedPNG writes a w x h grey image with faint grain and a transparent
// square in the middle, and with band a dark strip along the bottom: what
// the magick stub hands back for every call, so the Go steps have a hole and
// a ground to work on.
func holedPNG(t *testing.T, dir string, w, h int, band bool) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2-5 || x >= w/2+5 || y < h/2-5 || y >= h/2+5 {
				v := uint8(120 + (x*7+y*13)%5)
				if band && y >= h-4 {
					v = 20
				}
				img.Set(x, y, color.NRGBA{v, v, v, 255})
			}
		}
	}
	p := filepath.Join(dir, "fixture.png")
	if err := raster.SavePNG(p, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInfill(t *testing.T) {
	for _, c := range []struct {
		grain string
		band  bool
	}{{"1", false}, {"0", false}, {"1", true}} {
		t.Run(fmt.Sprintf("grain %s band %v", c.grain, c.band), func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("FIXTURE", holedPNG(t, dir, 40, 30, c.band))
			log := enginetest.Stub(t, "magick", `for a in "$@"; do last=$a; done; cp "$FIXTURE" "$last"`)
			in, mask, out := pngOf(t, dir, "in.png", 40, 30), pngOf(t, dir, "m.png", 40, 30), filepath.Join(dir, "out.png")
			var o, e bytes.Buffer
			if code := Main(context.Background(), []string{"--mask", mask, "--levels", "30,6", "--grain", c.grain, in, out}, &o, &e); code != 0 {
				t.Fatalf("code %d: %s", code, e.String())
			}
			calls := enginetest.Calls(t, log)
			blurSrc := "holes.png"
			if c.band { // holes, edge stops, 2 blurs, fill, composite
				if len(calls) != 6 || filepath.Base(calls[1][len(calls[1])-1]) != "reached.png" {
					t.Fatalf("want an edge-stop call writing reached.png: %q", calls)
				}
				calls = append(calls[:1], calls[2:]...)
				blurSrc = "reached.png"
			}
			if len(calls) != 5 { // holes, 2 blurs, fill, composite
				t.Fatalf("%d magick calls, want 5: %q", len(calls), calls)
			}
			if !slices.Contains(calls[1], "0x30") || !slices.Contains(calls[2], "0x6") {
				t.Errorf("blur levels: %q %q", calls[1], calls[2])
			}
			if filepath.Base(calls[1][0]) != blurSrc || filepath.Base(calls[2][0]) != blurSrc {
				t.Errorf("blurs read %q and %q, want %s", calls[1][0], calls[2][0], blurSrc)
			}
			if slices.Contains(calls[3], "+noise") {
				t.Errorf("fill adds magick noise: %q", calls[3])
			}
			comp := calls[4]
			if filepath.Base(comp[0]) != "holes.png" || filepath.Base(comp[2]) != "fill.png" || comp[len(comp)-1] != out {
				t.Errorf("composite takes %q, want holes.png, the fill, then %s", comp, out)
			}
			pieces := slices.ContainsFunc(comp, func(a string) bool { return strings.HasPrefix(filepath.Base(a), "grain") })
			if pieces != (c.grain == "1") {
				t.Errorf("grain %s: composite has grained pieces = %v: %q", c.grain, pieces, comp)
			}
		})
	}
}

func TestInfillRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "magick", `exit 0`)
	in, out := pngOf(t, dir, "in.png", 40, 30), filepath.Join(dir, "out.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"--mask", pngOf(t, dir, "m.png", 41, 30), in, out}, &o, &e); code != 1 {
		t.Errorf("mismatched mask: code %d", code)
	}
	if code := Main(context.Background(), []string{"--mask", pngOf(t, dir, "m2.png", 40, 30), "--levels", "6,20", in, out}, &o, &e); code != 2 {
		t.Errorf("ascending levels: code %d", code)
	}
	os.WriteFile("imgkit.toml", []byte(`synthesis = "forbid"`), 0o644)
	e.Reset()
	if code := Main(context.Background(), []string{"--mask", pngOf(t, dir, "m3.png", 40, 30), in, out}, &o, &e); code != 1 || !strings.Contains(e.String(), "forbid") {
		t.Errorf("forbidden: code %d, %q", code, e.String())
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("magick ran for a refused infill")
	}
}

func TestInfillRefusesSameFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "magick", `exit 0`)
	in, mask := pngOf(t, dir, "in.png", 40, 30), pngOf(t, dir, "m.png", 40, 30)
	before, _ := os.ReadFile(in)
	mbefore, _ := os.ReadFile(mask)
	for name, args := range map[string][]string{
		"in==out":   {"--mask", mask, in, in},
		"mask==out": {"--mask", mask, in, mask},
	} {
		var o, e bytes.Buffer
		if code := Main(context.Background(), args, &o, &e); code != 1 || !strings.Contains(e.String(), "also an input") {
			t.Errorf("%s: code %d, %q", name, code, e.String())
		}
	}
	if after, _ := os.ReadFile(in); !bytes.Equal(before, after) {
		t.Error("source changed")
	}
	if after, _ := os.ReadFile(mask); !bytes.Equal(mbefore, after) {
		t.Error("mask changed")
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("magick ran")
	}
}

func TestInfillRefusesNegativeGrain(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "magick", `exit 0`)
	in, mask := pngOf(t, dir, "in.png", 40, 30), pngOf(t, dir, "m.png", 40, 30)
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"--mask", mask, "--grain", "-0.5", in, filepath.Join(dir, "out.png")}, &o, &e); code != 2 || !strings.Contains(e.String(), "--grain of 0 or more") {
		t.Errorf("code %d, %q", code, e.String())
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("magick ran")
	}
}
