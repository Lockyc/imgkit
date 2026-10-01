package inpaint

import (
	"bytes"
	"context"
	"image"
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

// magick copies its first argument to its last; uv plays iopaint, writing
// the image into --output under its own name.
func stubs(t *testing.T) string {
	enginetest.Stub(t, "magick", `for a in "$@"; do last=$a; done; cp "$1" "$last"`)
	return enginetest.Stub(t, "uv", `img=; out=
for a in "$@"; do case "$a" in --image=*) img=${a#--image=};; --output=*) out=${a#--output=};; esac; done
cp "$img" "$out/$(basename "$img")"`)
}

func TestInpaint(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := stubs(t)
	in, mask, out := pngOf(t, dir, "in.png", 40, 30), pngOf(t, dir, "m.png", 40, 30), filepath.Join(dir, "out.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"--mask", mask, in, out}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	c := enginetest.Calls(t, log)[0]
	for _, want := range []string{"--from", "iopaint==1.6.0", "--model=lama", "--device=cpu", "--mask=" + mask} {
		if !slices.Contains(c, want) {
			t.Errorf("args lack %s: %q", want, c)
		}
	}
	if _, err := os.Stat(out); err != nil {
		t.Error("no output")
	}
}

func TestInpaintRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := stubs(t)
	in, out := pngOf(t, dir, "in.png", 40, 30), filepath.Join(dir, "out.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"--mask", pngOf(t, dir, "m.png", 20, 15), in, out}, &o, &e); code != 1 || !strings.Contains(e.String(), "40x30") {
		t.Errorf("mismatched mask: code %d, %q", code, e.String())
	}
	os.WriteFile(filepath.Join(dir, "imgkit.toml"), []byte(`synthesis = "forbid"`), 0o644)
	e.Reset()
	if code := Main(context.Background(), []string{"--mask", pngOf(t, dir, "m2.png", 40, 30), in, out}, &o, &e); code != 1 || !strings.Contains(e.String(), "forbid") {
		t.Errorf("forbidden: code %d, %q", code, e.String())
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("iopaint ran for a refused inpaint")
	}
}

func TestInpaintRefusesSameFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := stubs(t)
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
		t.Error("an engine ran")
	}
}
