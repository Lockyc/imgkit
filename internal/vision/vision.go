// Package vision runs Apple Vision's foreground segmentation through a small
// Swift helper, compiled once per helper version into the cache. It runs
// on-device: no image leaves the machine.
package vision

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lockyc/imgkit/internal/engine"
)

//go:embed mask.swift
var source []byte

// Available refuses Vision off macOS.
func Available(goos string) error {
	if goos != "darwin" {
		return errors.New("--coarse vision needs macOS 14 or later (Apple Vision); use --coarse birefnet")
	}
	return nil
}

func helper(ctx context.Context) (string, error) {
	sum := sha256.Sum256(source)
	cache, err := engine.CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "vision", hex.EncodeToString(sum[:])[:16])
	bin := filepath.Join(dir, "imgkit-vision")
	if fi, err := os.Stat(bin); err == nil && fi.Mode()&0o111 != 0 {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src := filepath.Join(dir, "mask.swift")
	if err := os.WriteFile(src, source, 0o644); err != nil {
		return "", err
	}
	tmp := bin + ".tmp"
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "swiftc", Args: []string{"-O", src, "-o", tmp}, Inputs: []string{src}, Outputs: []string{tmp}, Timeout: 5 * time.Minute}); err != nil {
		return "", err
	}
	return bin, os.Rename(tmp, bin)
}

// Mask writes in's foreground, alpha as the mask, to out.
func Mask(ctx context.Context, in, out string) error {
	if err := Available(runtime.GOOS); err != nil {
		return err
	}
	if engine.SameFile(in, out) {
		return errors.New("input and output are the same file: " + in)
	}
	bin, err := helper(ctx)
	if err != nil {
		return err
	}
	_, err = engine.Run(ctx, engine.Cmd{Engine: "imgkit-vision", Path: bin, Args: []string{in, out}, Inputs: []string{in}, Outputs: []string{out}, Timeout: 5 * time.Minute})
	return err
}
