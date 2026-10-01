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
)

func TestUpscale(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "realesrgan-ncnn-vulkan", `while [ "$1" != "-o" ]; do shift; done; printf png > "$2"`)
	out := filepath.Join(dir, "o.png")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"in.png", out}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	c := enginetest.Calls(t, log)[0]
	i := slices.Index(c, "-m")
	if i < 0 || filepath.Base(c[i+1]) != "models" || !slices.Contains(c, "realesrgan-x4plus") {
		t.Errorf("args %q", c)
	}
	os.WriteFile("imgkit.toml", []byte(`synthesis = "forbid"`), 0o644)
	e.Reset()
	if code := Main(context.Background(), []string{"in.png", out}, &o, &e); code != 1 || !strings.Contains(e.String(), "forbid") {
		t.Errorf("forbidden: code %d, %q", code, e.String())
	}
}

func TestUpscaleRefusesSameFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := enginetest.Stub(t, "realesrgan-ncnn-vulkan", `printf png > /dev/null`)
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
