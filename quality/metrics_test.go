package quality

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/lockyc/imgkit/internal/raster"
)

func TestPixelAndDPI(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(9, 9, color.RGBA{0, 204, 0, 255})
	raster.SavePNG(p, img)
	v, err := Metrics["pixel"](Params{"image": p, "at": "0.99,0.99", "color": "#00cc00"})
	if err != nil || v != 0 {
		t.Fatalf("pixel = %v, %v", v, err)
	}
	if v, _ := Metrics["pixel"](Params{"image": p, "at": "0,0", "color": "#00cc00"}); v < 0.4 {
		t.Errorf("pixel at a black corner = %v", v)
	}
	raster.SetDPI(p, 192)
	if v, _ := Metrics["dpi"](Params{"image": p}); v < 191.99 || v > 192.01 {
		t.Errorf("dpi = %v", v)
	}
}

func TestPixelRejectsOutOfRangeAt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	raster.SavePNG(p, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	for _, at := range []string{"-0.1,0", "0,1.5", "2,2", "NaN,0", "0,NaN"} {
		if _, err := Metrics["pixel"](Params{"image": p, "at": at, "color": "#000000"}); err == nil {
			t.Errorf("at %q accepted", at)
		}
	}
}

func TestGreenFringe(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.png")
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.NRGBA{200, 150, 100, 255})
		}
	}
	img.Set(0, 0, color.NRGBA{60, 160, 50, 255}) // green, opaque
	img.Set(1, 0, color.NRGBA{60, 160, 50, 0})   // green, transparent: ignored
	raster.SavePNG(p, img)
	v, err := Metrics["green-fringe"](Params{"image": p, "region": "0,0,1,1", "bg": "#062d5f"})
	if err != nil || v < 0.0099 || v > 0.0101 {
		t.Fatalf("green-fringe = %v, %v; want 0.01", v, err)
	}
}

func TestSeethrough(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.png")
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			a := uint8(255)
			if x < 5 {
				a = 100
			}
			img.Set(x, y, color.NRGBA{9, 9, 9, a})
		}
	}
	raster.SavePNG(p, img)
	v, err := Metrics["seethrough"](Params{"image": p, "solid": "0,0 1,0 1,1 0,1"})
	if err != nil || v < 0.49 || v > 0.51 {
		t.Fatalf("seethrough = %v, %v; want 0.5", v, err)
	}
}

func TestStdRatio(t *testing.T) {
	dir := t.TempDir()
	flat, noisy, mask := filepath.Join(dir, "f.png"), filepath.Join(dir, "n.png"), filepath.Join(dir, "m.png")
	fi, ni, mi := image.NewGray(image.Rect(0, 0, 40, 40)), image.NewGray(image.Rect(0, 0, 40, 40)), image.NewGray(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			fi.SetGray(x, y, color.Gray{128})
			ni.SetGray(x, y, color.Gray{uint8(100 + 56*((x+y)%2))})
			if x >= 15 && x < 25 && y >= 15 && y < 25 {
				mi.SetGray(x, y, color.Gray{255})
			}
		}
	}
	raster.SavePNG(flat, fi)
	raster.SavePNG(noisy, ni)
	raster.SavePNG(mask, mi)
	if v, _ := Metrics["std-ratio"](Params{"a": flat, "b": noisy, "mask": mask, "ring": int64(5)}); v != 0 {
		t.Errorf("flat fill ratio = %v, want 0", v)
	}
	if v, _ := Metrics["std-ratio"](Params{"a": noisy, "b": noisy, "mask": mask, "ring": int64(5)}); v < 0.99 || v > 1.01 {
		t.Errorf("same texture ratio = %v, want 1", v)
	}
}

func TestRMSEMask(t *testing.T) {
	dir := t.TempDir()
	a, b, mask := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png"), filepath.Join(dir, "m.png")
	ai, bi, mi := image.NewGray(image.Rect(0, 0, 4, 4)), image.NewGray(image.Rect(0, 0, 4, 4)), image.NewGray(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y < 2 {
				bi.SetGray(x, y, color.Gray{255}) // differs only where the mask is black
			} else {
				mi.SetGray(x, y, color.Gray{255})
			}
		}
	}
	raster.SavePNG(a, ai)
	raster.SavePNG(b, bi)
	raster.SavePNG(mask, mi)
	if v, err := Metrics["rmse"](Params{"a": a, "b": b, "mask": mask}); err != nil || v != 0 {
		t.Errorf("masked rmse = %v, %v; want 0", v, err)
	}
	if v, _ := Metrics["rmse"](Params{"a": a, "b": b}); v == 0 {
		t.Error("unmasked rmse = 0, want the top-half difference")
	}
}
