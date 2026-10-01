package upscale

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
	"github.com/lockyc/imgkit/internal/pins"
)

// magickStub writes "frame" to the frame path, less its PNG32: prefix.
const magickStub = `for a in "$@"; do last=$a; done; printf frame > "${last#PNG32:}"`

func stubs(t *testing.T) (magick, uv string) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return enginetest.Stub(t, "magick", magickStub),
		enginetest.Stub(t, "uv", `while [ "$1" != "--out" ]; do shift; done; printf png > "$2"`)
}

func TestUpscale(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	magick, uv := stubs(t)
	out := filepath.Join(dir, "o.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"in.png", out}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	f := enginetest.Calls(t, magick)[0]
	frame := strings.TrimPrefix(f[len(f)-1], "PNG32:")
	c := enginetest.Calls(t, uv)[0]
	if !slices.ContainsFunc(c, func(a string) bool { return strings.HasSuffix(a, "upscale.py") }) {
		t.Errorf("no upscale.py in %q", c)
	}
	for _, want := range []string{"--image", frame, "--out", out, "--model", pins.DAT.Repo, "--revision", pins.DAT.Revision, "--file", pins.DAT.File} {
		if !slices.Contains(c, want) {
			t.Errorf("args lack %s: %q", want, c)
		}
	}
}

// TestFrameOrder: the model reads the source oriented, then in sRGB, then
// stripped, at full size, alpha kept, as 8-bit RGBA.
func TestFrameOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	magick, _ := stubs(t)
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"in.jpg", filepath.Join(dir, "o.png")}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	f := enginetest.Calls(t, magick)[0]
	idx := func(s string) int { return slices.Index(f, s) }
	if f[0] != "in.jpg" || !(0 < idx("-auto-orient") && idx("-auto-orient") < idx("-profile") && idx("-profile") < idx("-strip") && idx("-strip") < idx("-depth")) {
		t.Errorf("frame args out of order: %q", f)
	}
	if slices.Contains(f, "-resize") || slices.Contains(f, "off") || !strings.HasPrefix(f[len(f)-1], "PNG32:") {
		t.Errorf("frame must keep size and alpha as RGBA: %q", f)
	}
}

func TestUpscaleRefusesSameFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	magick, uv := stubs(t)
	in := filepath.Join(dir, "in.png")
	os.WriteFile(in, []byte("source"), 0o644)
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{in, in}, &o, &e); code != 1 || !strings.Contains(e.String(), "both an input and an output") {
		t.Errorf("code %d, %q", code, e.String())
	}
	if b, _ := os.ReadFile(in); string(b) != "source" {
		t.Error("source changed")
	}
	if len(enginetest.Calls(t, magick))+len(enginetest.Calls(t, uv)) != 0 {
		t.Error("an engine ran")
	}
}
