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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lockyc/imgkit/internal/engine"
)

//go:embed mask.swift
var source []byte

// Available refuses Vision where it cannot run: off macOS, or on a macOS
// before 14, whose Vision lacks foreground instance masks.
func Available() error { return available(runtime.GOOS, macOSVersion()) }

// available is Available for a given OS and macOS version ("" when unknown,
// which leaves swiftc and Vision to speak for themselves).
func available(goos, version string) error {
	if goos != "darwin" {
		return errors.New("--coarse vision needs macOS 14 or later (Apple Vision); use --coarse birefnet")
	}
	major, _, _ := strings.Cut(version, ".")
	if n, err := strconv.Atoi(major); err == nil && n < 14 {
		return fmt.Errorf("--coarse vision needs macOS 14 or later (Apple Vision), and this is macOS %s; use --coarse birefnet", version)
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
	// A build directory of its own, so concurrent runs never write one file.
	build, err := os.MkdirTemp(dir, ".build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(build)
	src, out := filepath.Join(build, "mask.swift"), filepath.Join(build, "imgkit-vision")
	if err := os.WriteFile(src, source, 0o644); err != nil {
		return "", err
	}
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "swiftc", Args: []string{"-O", src, "-o", out}, Inputs: []string{src}, Outputs: []string{out}, Timeout: 5 * time.Minute}); err != nil {
		return "", err
	}
	return bin, os.Rename(out, bin)
}

// Mask writes in's foreground, alpha as the mask, to out.
func Mask(ctx context.Context, in, out string) error {
	if err := Available(); err != nil {
		return err
	}
	bin, err := helper(ctx)
	if err != nil {
		return err
	}
	_, err = engine.Run(ctx, engine.Cmd{Engine: "imgkit-vision", Path: bin, Args: []string{in, out}, Inputs: []string{in}, Outputs: []string{out}, Timeout: 5 * time.Minute})
	return err
}
