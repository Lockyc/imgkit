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
