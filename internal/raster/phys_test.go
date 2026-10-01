package raster

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSetDPI(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	if err := SavePNG(p, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	if d, _ := DPI(p); d != 0 {
		t.Fatalf("fresh PNG reports %v dpi", d)
	}
	for _, want := range []float64{192, 144} {
		if err := SetDPI(p, want); err != nil {
			t.Fatal(err)
		}
		got, err := DPI(p)
		if err != nil || math.Abs(got-want) > 0.01 {
			t.Fatalf("DPI = %v, %v; want %v", got, err, want)
		}
	}
	b, _ := os.ReadFile(p)
	if n := bytes.Count(b, []byte("pHYs")); n != 1 {
		t.Errorf("%d pHYs chunks after two stamps, want 1", n)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("stamped PNG no longer decodes: %v", err)
	}
}

func TestSetDPIRejectsNonPNG(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	os.WriteFile(p, []byte("GIF89a"), 0o644)
	if err := SetDPI(p, 96); err == nil {
		t.Fatal("non-PNG accepted")
	}
}
