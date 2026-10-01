package cutout

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
	"github.com/lockyc/imgkit/internal/pins"
	"github.com/lockyc/imgkit/internal/raster"
)

// magick: `-format %wx%h info:` prints $SRC_DIMS; any other call copies
// $FIXTURE_FRAME to its last argument.
const magickStub = `case "$*" in
  *info:*) printf '%s' "${SRC_DIMS:-800x1200}" ;;
  *) for a in "$@"; do last=$a; done; cp "$FIXTURE_FRAME" "$last" ;;
esac`

// uv: birefnet.py copies $FIXTURE_MASK to its 2nd argument; matte.py writes
// $FIXTURE_FRAME to --out, or fails with $MATTE_FAIL.
const uvStub = `script=$4; shift 4
case "$script" in
  *birefnet.py) cp "$FIXTURE_MASK" "$2" ;;
  *matte.py)
    [ -n "$MATTE_FAIL" ] && { echo "$MATTE_FAIL" >&2; exit 1; }
    while [ "$1" != "--out" ]; do shift; done; cp "$FIXTURE_FRAME" "$2" ;;
esac`

func png(t *testing.T, fill color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, fill)
		}
	}
	p := filepath.Join(t.TempDir(), "f.png")
	raster.SavePNG(p, img)
	return p
}

type env struct {
	magick, uv, in, out string
	stdout, stderr      bytes.Buffer
}

func setup(t *testing.T) *env {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	e := &env{magick: enginetest.Stub(t, "magick", magickStub), uv: enginetest.Stub(t, "uv", uvStub)}
	t.Setenv("FIXTURE_FRAME", png(t, color.RGBA{200, 150, 100, 255}))
	t.Setenv("FIXTURE_MASK", png(t, color.White))
	t.Setenv("SRC_DIMS", "800x1200")
	t.Setenv("MATTE_FAIL", "")
	dir := t.TempDir()
	e.in, e.out = filepath.Join(dir, "in.jpg"), filepath.Join(dir, "out.png")
	os.WriteFile(e.in, []byte("jpg"), 0o644)
	return e
}

func (e *env) run(args ...string) int {
	return Main(context.Background(), append(args, e.in, e.out), &e.stdout, &e.stderr)
}

func TestCutoutBirefnet(t *testing.T) {
	e := setup(t)
	if code := e.run("--coarse", "birefnet", "--height", "600"); code != 0 {
		t.Fatalf("code %d: %s", code, e.stderr.String())
	}
	calls := enginetest.Calls(t, e.uv)
	matte := calls[len(calls)-1]
	for _, want := range []string{"--model", pins.ViTMatte.Repo, "--revision", pins.ViTMatte.Revision, "--tile", "1024", "--band-in", "60", "--band-out", "40"} {
		if !slices.Contains(matte, want) {
			t.Errorf("matte args lack %s: %q", want, matte)
		}
	}
	if slices.Contains(matte, "--despill-hue") {
		t.Error("despill ran without being asked for")
	}
}

func TestFrameOrder(t *testing.T) {
	e := setup(t)
	e.run("--coarse", "birefnet", "--height", "600")
	var frame []string
	for _, c := range enginetest.Calls(t, e.magick) {
		if slices.Contains(c, "-strip") {
			frame = c
		}
	}
	idx := func(s string) int { return slices.Index(frame, s) }
	if !(idx("-auto-orient") < idx("-profile") && idx("-profile") < idx("-strip") && idx("-strip") < idx("-resize")) {
		t.Errorf("frame args out of order: %q", frame)
	}
	if !slices.Contains(frame, "x600") {
		t.Errorf("no resize to the working height: %q", frame)
	}
	if first := enginetest.Calls(t, e.magick)[0]; slices.Index(first, "-auto-orient") < 0 {
		t.Errorf("dimensions read before orienting: %q", first)
	}
}

func TestCutoutRefusesUpsample(t *testing.T) {
	e := setup(t)
	if code := e.run("--coarse", "birefnet", "--height", "1300"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(e.stderr.String(), "upsample") {
		t.Errorf("stderr %q", e.stderr.String())
	}
	if len(enginetest.Calls(t, e.uv)) != 0 {
		t.Error("a model ran for a refused cut-out")
	}
}

func TestCutoutNoForeground(t *testing.T) {
	e := setup(t)
	t.Setenv("FIXTURE_MASK", png(t, color.Black))
	if code := e.run("--coarse", "birefnet"); code != 1 || !strings.Contains(e.stderr.String(), "no foreground") {
		t.Fatalf("code %d, stderr %q", code, e.stderr.String())
	}
}

func TestCutoutScriptFailureLeavesNothing(t *testing.T) {
	e := setup(t)
	os.WriteFile(e.out, []byte("stale"), 0o644)
	t.Setenv("MATTE_FAIL", "OSError: couldn't connect to huggingface.co")
	if code := e.run("--coarse", "birefnet"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(e.stderr.String(), "uv") || !strings.Contains(e.stderr.String(), "huggingface") {
		t.Errorf("stderr %q", e.stderr.String())
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Error("output left behind")
	}
}

func TestDespillPreset(t *testing.T) {
	e := setup(t)
	if code := e.run("--coarse", "birefnet", "--despill", "warm-on-green", "--despill-lmax", "80"); code != 0 {
		t.Fatalf("code %d: %s", code, e.stderr.String())
	}
	calls := enginetest.Calls(t, e.uv)
	m := calls[len(calls)-1]
	pairs := map[string]string{"--despill-hue": "70:100", "--despill-hue-end": "200", "--despill-chroma": "6:40", "--despill-lmax": "80", "--despill-clean": "15:65"}
	for k, v := range pairs {
		if i := slices.Index(m, k); i < 0 || m[i+1] != v {
			t.Errorf("%s = %q, want %s", k, m, v)
		}
	}
}

func TestCutoutUsage(t *testing.T) {
	e := setup(t)
	for _, args := range [][]string{{"--coarse", "sam"}, {"--despill", "nope"}, {"--despill-hue", "70"}} {
		e.stderr.Reset()
		if code := e.run(args...); code != 2 {
			t.Errorf("%q: code %d", args, code)
		}
	}
	var o, errb bytes.Buffer
	if code := Main(context.Background(), []string{e.in, filepath.Join(t.TempDir(), "o.webp")}, &o, &errb); code != 2 {
		t.Errorf("webp out: code %d", code)
	}
}

func TestInputEqualToOutputIsRefused(t *testing.T) {
	e := setup(t)
	same := filepath.Join(t.TempDir(), "in.png")
	os.WriteFile(same, []byte("png"), 0o644)
	var o, errb bytes.Buffer
	if code := Main(context.Background(), []string{"--coarse", "birefnet", same, same}, &o, &errb); code != 1 {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	if b, err := os.ReadFile(same); err != nil || string(b) != "png" {
		t.Errorf("source did not survive: %q %v", b, err)
	}
	if c := enginetest.Calls(t, e.uv); len(c) != 0 {
		t.Errorf("a model ran for a refused cut-out: %q", c)
	}
}
