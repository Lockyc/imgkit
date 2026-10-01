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

func TestUpscale(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "uv", `while [ "$1" != "--out" ]; do shift; done; printf png > "$2"`)
	out := filepath.Join(dir, "o.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"in.png", out}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	c := enginetest.Calls(t, log)[0]
	if !strings.HasSuffix(c[3], "upscale.py") {
		t.Errorf("script %q", c[3])
	}
	for _, want := range []string{"--image", "in.png", "--out", out, "--model", pins.DAT.Repo, "--revision", pins.DAT.Revision, "--file", pins.DAT.File} {
		if !slices.Contains(c, want) {
			t.Errorf("args lack %s: %q", want, c)
		}
	}
}

func TestUpscaleForbidden(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	log := enginetest.Stub(t, "uv", `exit 0`)
	os.WriteFile("imgkit.toml", []byte(`synthesis = "forbid"`), 0o644)
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"in.png", "o.png"}, &o, &e); code != 1 || !strings.Contains(e.String(), "forbid") {
		t.Errorf("forbidden: code %d, %q", code, e.String())
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("engine ran")
	}
}

func TestUpscaleRefusesSameFile(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "uv", `exit 0`)
	in := filepath.Join(dir, "in.png")
	os.WriteFile(in, []byte("source"), 0o644)
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{in, in}, &o, &e); code != 1 || !strings.Contains(e.String(), "both an input and an output") {
		t.Errorf("code %d, %q", code, e.String())
	}
	if b, _ := os.ReadFile(in); string(b) != "source" {
		t.Error("source changed")
	}
	if len(enginetest.Calls(t, log)) != 0 {
		t.Error("engine ran")
	}
}
