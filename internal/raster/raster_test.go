package raster

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func solid(w, h int, c color.Color) *image.RGBA64 {
	img := image.NewRGBA64(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRMSE(t *testing.T) {
	black, white := solid(4, 4, color.Black), solid(4, 4, color.White)
	if v, _ := RMSE(black, black, black.Bounds(), RGB); v != 0 {
		t.Errorf("identical = %v", v)
	}
	if v, _ := RMSE(black, white, black.Bounds(), RGB); math.Abs(v-1) > 1e-9 {
		t.Errorf("black vs white = %v, want 1", v)
	}
	half := solid(4, 4, color.Black)
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			half.Set(x, y, color.White)
		}
	}
	top := Frac{0, 0, 1, 0.5}.In(half.Bounds())
	if v, _ := RMSE(half, white, top, RGB); v != 0 {
		t.Errorf("top half = %v, want 0", v)
	}
	clear := solid(4, 4, color.Transparent)
	if v, _ := RMSE(clear, white, clear.Bounds(), Alpha); math.Abs(v-1) > 1e-9 {
		t.Errorf("alpha = %v, want 1", v)
	}
	if _, err := RMSE(black, solid(5, 4, color.Black), black.Bounds(), RGB); err == nil {
		t.Error("size mismatch accepted")
	}
}

func TestRMSEMasked(t *testing.T) {
	black, white := solid(4, 4, color.Black), solid(4, 4, color.White)
	mask := solid(4, 4, color.Black)
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			mask.Set(x, y, color.White)
		}
	}
	if v, err := RMSEMasked(black, white, mask, RGB); err != nil || math.Abs(v-1) > 1e-9 {
		t.Errorf("half mask, black vs white = %v, %v; want 1", v, err)
	}
	if _, err := RMSEMasked(black, white, solid(4, 4, color.Black), RGB); err == nil {
		t.Error("an all-black mask was accepted")
	}
}

func TestParseFrac(t *testing.T) {
	f, err := ParseFrac("0,0.88,1,0.12")
	if err != nil || f != (Frac{0, 0.88, 1, 0.12}) {
		t.Fatalf("ParseFrac = %v, %v", f, err)
	}
	for _, bad := range []string{"", "1,2,3", "0,0,1.5,1", "a,b,c,d", "-0.1,0,1,1", "NaN,0,1,1"} {
		if _, err := ParseFrac(bad); err == nil {
			t.Errorf("ParseFrac(%q) accepted", bad)
		}
	}
	r := Frac{0.5, 0.5, 0.5, 0.5}.In(image.Rect(0, 0, 100, 40))
	if r != image.Rect(50, 20, 100, 40) {
		t.Errorf("In = %v", r)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	src := solid(3, 2, color.NRGBA{200, 100, 50, 128})
	if err := SavePNG(p, src); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := RMSE(src, got, src.Bounds(), RGB); v > 1e-3 {
		t.Errorf("round trip RMSE %v", v)
	}
}

func TestSavePNGFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	fresh, existing := filepath.Join(dir, "fresh.png"), filepath.Join(dir, "old.png")
	os.WriteFile(existing, []byte("old"), 0o644)
	empty := image.NewRGBA64(image.Rect(0, 0, 0, 0)) // png.Encode rejects a zero-size image
	for _, p := range []string{fresh, existing} {
		if err := SavePNG(p, empty); err == nil {
			t.Fatalf("SavePNG(%s) encoded an empty image", p)
		}
	}
	if b, _ := os.ReadFile(existing); string(b) != "old" {
		t.Errorf("existing file = %q, want it untouched", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir holds %d entries, want only old.png", len(entries))
	}
}
