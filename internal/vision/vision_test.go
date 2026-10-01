package vision

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
)

func TestVisionOffMac(t *testing.T) {
	err := Available("linux")
	if err == nil || !strings.Contains(err.Error(), "--coarse birefnet") {
		t.Fatalf("Available(linux) = %v", err)
	}
	if Available("darwin") != nil {
		t.Fatal("darwin refused")
	}
}

func TestMaskCompilesOnceAndRuns(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Vision is macOS-only")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// The stub compiler writes a helper that copies its input to its output.
	log := enginetest.Stub(t, "swiftc", `while [ "$1" != "-o" ]; do shift; done
printf '#!/bin/sh\ncp "$1" "$2"\n' > "$2"; chmod +x "$2"`)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.png"), filepath.Join(dir, "out.png")
	os.WriteFile(in, []byte("png"), 0o644)
	for i := 0; i < 2; i++ {
		if err := Mask(context.Background(), in, out); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(enginetest.Calls(t, log)); n != 1 {
		t.Errorf("swiftc ran %d times, want 1", n)
	}
	if b, _ := os.ReadFile(out); string(b) != "png" {
		t.Error("helper did not run")
	}
}

func TestMaskRefusesSameFile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Vision is macOS-only")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	enginetest.Stub(t, "swiftc", `while [ "$1" != "-o" ]; do shift; done
printf '#!/bin/sh\ncp "$1" "$2"\n' > "$2"; chmod +x "$2"`)
	in := filepath.Join(t.TempDir(), "in.png")
	os.WriteFile(in, []byte("png"), 0o644)
	if err := Mask(context.Background(), in, in); err == nil {
		t.Fatal("in == out accepted")
	}
	if b, _ := os.ReadFile(in); string(b) != "png" {
		t.Error("source did not survive")
	}
}

// TestRealVision compiles the helper with the real swiftc and cuts a real
// image. Opt-in (IMGKIT_REAL=1): it is not part of gate or CI.
func TestRealVision(t *testing.T) {
	if os.Getenv("IMGKIT_REAL") == "" || runtime.GOOS != "darwin" {
		t.Skip("set IMGKIT_REAL=1 on macOS")
	}
	in := os.Getenv("IMGKIT_REAL_IN")
	if in == "" {
		t.Skip("set IMGKIT_REAL_IN to a PNG")
	}
	out := filepath.Join(t.TempDir(), "out.png")
	if err := Mask(context.Background(), in, out); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", out)
	if dst := os.Getenv("IMGKIT_REAL_OUT"); dst != "" {
		b, _ := os.ReadFile(out)
		os.WriteFile(dst, b, 0o644)
	}
}
