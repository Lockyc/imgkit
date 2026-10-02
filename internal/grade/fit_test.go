package grade

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/plate/internal/enginetest"
)

// frameStub plays magick writing a frame: "frame" to its last argument,
// less the PNG24:/PNG32: prefix.
const frameStub = `for a in "$@"; do last=$a; done; last=${last#PNG24:}; printf frame > "${last#PNG32:}"`

func TestFit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	magick := enginetest.Stub(t, "magick", frameStub)
	log := enginetest.Stub(t, "uv", `while [ "$1" != "--out" ]; do shift; done; printf png > "$2"`)
	out := filepath.Join(t.TempDir(), "h.png")
	var o, e bytes.Buffer
	code := Main(context.Background(), []string{"fit", "--ref", "r.png", "--ref-crop", "836,0,700,1024", "--exclude", "0,680,700,344", "s.png", out}, &o, &e)
	if code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	// Both images are normalised frames, so --ref-crop is in displayed
	// orientation and the fit maps sRGB to sRGB: the subject keeps its
	// alpha, the reference is opaque.
	m := enginetest.Calls(t, magick)
	if len(m) != 2 || m[0][0] != "s.png" || m[1][0] != "r.png" {
		t.Fatalf("want the subject's frame, then the reference's: %q", m)
	}
	for i, prefix := range []string{"PNG32:", "PNG24:"} {
		idx := func(s string) int { return slices.Index(m[i], s) }
		if !(0 < idx("-auto-orient") && idx("-auto-orient") < idx("-profile") && idx("-profile") < idx("-strip")) || !strings.HasPrefix(m[i][len(m[i])-1], prefix) || slices.Contains(m[i], "-resize") {
			t.Errorf("frame %d args %q", i, m[i])
		}
	}
	subject, ref := strings.TrimPrefix(m[0][len(m[0])-1], "PNG32:"), strings.TrimPrefix(m[1][len(m[1])-1], "PNG24:")
	c := enginetest.Calls(t, log)[0]
	for _, want := range []string{"--subject", subject, "--ref", ref, "--out", out, "--ref-crop", "836,0,700,1024", "--exclude", "0,680,700,344", "--level", "8"} {
		if !slices.Contains(c, want) {
			t.Errorf("args lack %s: %q", want, c)
		}
	}
	for _, bad := range [][]string{{"fit", "s.png", out}, {"fit", "--ref", "r.png", "--ref-crop", "1,2,3", "s.png", out}} {
		if code := Main(context.Background(), bad, &o, &e); code != 2 {
			t.Errorf("%q: code %d", bad, code)
		}
	}
}

func TestFitRefuseSameFile(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	magick := enginetest.Stub(t, "magick", frameStub)
	log := enginetest.Stub(t, "uv", `exit 0`)
	dir := t.TempDir()
	s := filepath.Join(dir, "s.png")
	r := filepath.Join(dir, "r.png")
	for _, f := range []string{s, r} {
		if err := os.WriteFile(f, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"fit", "--ref", r, s, s}, {"fit", "--ref", r, s, r}} {
		var o, e bytes.Buffer
		if code := Main(context.Background(), args, &o, &e); code != 1 || !strings.Contains(e.String(), "also an input") {
			t.Errorf("%q: code %d, %q", args, code, e.String())
		}
	}
	for _, f := range []string{s, r} {
		if b, err := os.ReadFile(f); err != nil || string(b) != "png" {
			t.Errorf("%s did not survive: %q %v", f, b, err)
		}
	}
	if c := append(enginetest.Calls(t, magick), enginetest.Calls(t, log)...); len(c) != 0 {
		t.Errorf("an engine ran despite the refusal: %q", c)
	}
}
