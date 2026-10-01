// Package ml runs imgkit's Python: embedded single-file scripts whose
// dependencies are fixed by a committed `uv lock --script` lockfile and run
// with --locked, and pinned upstream CLIs through `uv tool run`. uv is the
// only Python tool a user installs. Python is here only because the matting,
// inpainting and super-resolution models have no Go, Rust or shell
// equivalent of comparable quality.
package ml

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/pins"
)

//go:embed scripts
var scripts embed.FS

// Timeout covers a first run, which downloads the dependencies and model weights.
const Timeout = 2 * time.Hour

// Script writes an embedded script and its lockfile into the cache and
// returns the script's path. The directory is named by a hash of both, so a
// new imgkit never runs a script an older one left behind.
func Script(name string) (string, error) {
	py, err := scripts.ReadFile("scripts/" + name)
	if err != nil {
		return "", fmt.Errorf("no embedded script %s", name)
	}
	lock, err := scripts.ReadFile("scripts/" + name + ".lock")
	if err != nil {
		return "", fmt.Errorf("%s has no lockfile", name)
	}
	h := sha256.New()
	h.Write(py)
	h.Write(lock)
	cache, err := engine.CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "ml", hex.EncodeToString(h.Sum(nil))[:16])
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".ml-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, name+".lock"), lock, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, name), py, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil && !os.IsExist(err) {
		if _, statErr := os.Stat(path); statErr != nil {
			return "", err
		}
	}
	return path, nil
}

// RunScript runs an embedded script with uv, locked to its lockfile.
func RunScript(ctx context.Context, name string, args, inputs, outputs []string) (engine.Result, error) {
	p, err := Script(name)
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Run(ctx, engine.Cmd{Engine: "uv", Args: append([]string{"run", "--locked", "--script", p}, args...), Inputs: inputs, Outputs: outputs, Timeout: Timeout})
}

// RunTool runs cmd from a pinned upstream package.
func RunTool(ctx context.Context, t pins.PyTool, cmd string, args, inputs, outputs []string) (engine.Result, error) {
	a := append([]string{"tool", "run", "--from", t.Package + "==" + t.Version, "--exclude-newer", t.ExcludeNewer, cmd}, args...)
	return engine.Run(ctx, engine.Cmd{Engine: "uv", Args: a, Inputs: inputs, Outputs: outputs, Timeout: Timeout})
}
