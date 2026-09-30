package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/enginetest"
	"github.com/lockyc/imgkit/internal/pins"
)

var ctx = context.Background()

func TestRunPassesArgsVerbatim(t *testing.T) {
	log := enginetest.Stub(t, "magick", `printf ok > "$1"`)
	out := filepath.Join(t.TempDir(), "out.png")
	args := []string{out, "a b", "$(rm -rf /)", "x;y", "*"}
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: args, Outputs: []string{out}}); err != nil {
		t.Fatal(err)
	}
	calls := enginetest.Calls(t, log)
	if len(calls) != 1 || !slices.Equal(calls[0], args) {
		t.Fatalf("calls = %q, want one call with %q", calls, args)
	}
}

func TestRunRemovesStaleOutputAndFailsWhenNoneWritten(t *testing.T) {
	enginetest.Stub(t, "magick", `exit 0`)
	out := filepath.Join(t.TempDir(), "out.png")
	os.WriteFile(out, []byte("stale"), 0o644)
	_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Outputs: []string{out}})
	var re *engine.RunError
	if !errors.As(err, &re) || !strings.Contains(re.Reason, "produced no output") {
		t.Fatalf("err = %v, want RunError 'produced no output'", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("stale output survived")
	}
}

func TestRunEmptyOutputIsFailure(t *testing.T) {
	enginetest.Stub(t, "magick", `: > "$1"`)
	out := filepath.Join(t.TempDir(), "out.png")
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{out}, Outputs: []string{out}}); err == nil {
		t.Fatal("empty output accepted")
	}
}

func TestRunExitStatusFailsAndRemovesOutput(t *testing.T) {
	enginetest.Stub(t, "magick", `printf partial > "$1"; echo boom >&2; exit 3`)
	out := filepath.Join(t.TempDir(), "out.png")
	_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{out}, Outputs: []string{out}})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("output of a failed run survived")
	}
}

func TestRunOKExit(t *testing.T) {
	enginetest.Stub(t, "magick", `exit 1`)
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "magick", OKExit: []int{1}}); err != nil {
		t.Fatal(err)
	}
}

func TestRunStderrPolicy(t *testing.T) {
	enginetest.Stub(t, "gs", `echo "   **** Error: rangecheck" >&2`)
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "gs"}); err != nil {
		t.Fatalf("stderr was fatal without StderrFatal: %v", err)
	}
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "gs", StderrFatal: true}); err == nil {
		t.Fatal("stderr ignored under StderrFatal")
	}
}

func TestRunTimeoutRemovesPartialOutput(t *testing.T) {
	enginetest.Stub(t, "magick", `printf partial > "$1"; sleep 30 & wait`)
	out := filepath.Join(t.TempDir(), "out.png")
	start := time.Now()
	_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{out}, Outputs: []string{out}, Timeout: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatal("the process group was not killed; Run waited for the child")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("partial output survived a timeout")
	}
}

func TestResolveNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(engine.EnvOverride("magick"), "")
	_, err := engine.Resolve("magick")
	var ni *engine.NotInstalledError
	if !errors.As(err, &ni) {
		t.Fatalf("err = %v, want NotInstalledError", err)
	}
	e, _ := pins.Lookup("magick")
	if ni.Hint != e.Hint() {
		t.Errorf("hint %q, want the pin table's %q", ni.Hint, e.Hint())
	}
}

func TestResolveManaged(t *testing.T) {
	e, _ := pins.Lookup("chrome-headless-shell")
	a, ok := e.Asset()
	if !ok {
		t.Skip("no chrome-headless-shell build for " + runtime.GOOS + "/" + runtime.GOARCH)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(engine.EnvOverride(e.Name), "")
	if _, err := engine.Resolve(e.Name); err == nil {
		t.Fatal("resolved before install")
	}
	dir, _ := engine.ManagedDir(e)
	bin := filepath.Join(dir, a.Bin)
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	got, err := engine.Resolve(e.Name)
	if err != nil || got != bin {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, bin)
	}
}

func TestVersionIgnoresExitStatus(t *testing.T) {
	enginetest.Stub(t, "pdfinfo", `echo "pdfinfo version 26.09.0" >&2; exit 99`)
	e, _ := pins.Lookup("pdfinfo")
	v, err := engine.Version(ctx, e)
	if err != nil || v != "26.09.0" {
		t.Fatalf("Version = %q, %v", v, err)
	}
}
