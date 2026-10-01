package grade

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
)

func TestFit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	log := enginetest.Stub(t, "uv", `while [ "$1" != "--out" ]; do shift; done; printf png > "$2"`)
	out := filepath.Join(t.TempDir(), "h.png")
	var o, e bytes.Buffer
	code := Main(context.Background(), []string{"fit", "--ref", "r.png", "--ref-crop", "836,0,700,1024", "--exclude", "0,680,700,344", "s.png", out}, &o, &e)
	if code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	c := enginetest.Calls(t, log)[0]
	for _, want := range []string{"--subject", "s.png", "--ref", "r.png", "--out", out, "--ref-crop", "836,0,700,1024", "--exclude", "0,680,700,344", "--level", "8"} {
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
		if code := Main(context.Background(), args, &o, &e); code == 0 {
			t.Errorf("%q accepted", args)
		}
	}
	for _, f := range []string{s, r} {
		if b, err := os.ReadFile(f); err != nil || string(b) != "png" {
			t.Errorf("%s did not survive: %q %v", f, b, err)
		}
	}
	if c := enginetest.Calls(t, log); len(c) != 0 {
		t.Errorf("uv ran despite the refusal: %q", c)
	}
}
