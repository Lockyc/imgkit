package infill

import (
	"bytes"
	"context"
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

// holedPNG writes a w x h grey image with grain and a transparent square in
// the middle: what the magick stub hands back for every call, so the Go
// grain step has a hole and a ground to work on.
func holedPNG(t *testing.T, dir string, w, h int) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2-5 || x >= w/2+5 || y < h/2-5 || y >= h/2+5 {
				v := uint8(100 + (x*7+y*13)%40)
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
	for _, grain := range []string{"1", "0"} {
		t.Run("grain "+grain, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("FIXTURE", holedPNG(t, dir, 40, 30))
			log := enginetest.Stub(t, "magick", `for a in "$@"; do last=$a; done; cp "$FIXTURE" "$last"`)
			in, mask, out := pngOf(t, dir, "in.png", 40, 30), pngOf(t, dir, "m.png", 40, 30), filepath.Join(dir, "out.png")
			var o, e bytes.Buffer
			if code := Main(context.Background(), []string{"--mask", mask, "--levels", "30,6", "--grain", grain, in, out}, &o, &e); code != 0 {
				t.Fatalf("code %d: %s", code, e.String())
			}
			calls := enginetest.Calls(t, log)
			if len(calls) != 5 { // holes, 2 blurs, fill, composite
				t.Fatalf("%d magick calls, want 5: %q", len(calls), calls)
			}
			if !slices.Contains(calls[1], "0x30") || !slices.Contains(calls[2], "0x6") {
				t.Errorf("blur levels: %q %q", calls[1], calls[2])
			}
			if slices.Contains(calls[3], "+noise") {
				t.Errorf("fill adds magick noise: %q", calls[3])
			}
			want := map[string]string{"1": "grained.png", "0": "fill.png"}[grain]
			comp := calls[4]
			if filepath.Base(comp[0]) != "holes.png" || filepath.Base(comp[1]) != want || comp[len(comp)-1] != out {
				t.Errorf("composite takes %q, want holes.png, %s, then %s", comp, want, out)
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
