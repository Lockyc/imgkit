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

func TestSetDPIValidation(t *testing.T) {
	tests := []struct {
		name string
		dpi  float64
	}{
		{"zero", 0},
		{"negative", -1},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "a.png")
			if err := SavePNG(p, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
				t.Fatal(err)
			}
			orig, _ := os.ReadFile(p)
			if err := SetDPI(p, tt.dpi); err == nil {
				t.Fatalf("SetDPI(%v) accepted", tt.dpi)
			}
			after, _ := os.ReadFile(p)
			if !bytes.Equal(orig, after) {
				t.Fatal("file was modified on error")
			}
		})
	}
}

func TestSetDPIPreservesMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	if err := SavePNG(p, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetDPI(p, 96); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 0644", fi.Mode().Perm())
	}
}

func TestSetDPINoIHDR(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	// Write PNG signature but no IHDR chunk
	os.WriteFile(p, []byte("\x89PNG\r\n\x1a\n"), 0o644)
	if err := SetDPI(p, 96); err == nil {
		t.Fatal("PNG without IHDR accepted")
	}
}
