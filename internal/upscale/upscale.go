// Package upscale enlarges an image 4× with DAT (internal/ml/scripts/
// upscale.py), for artwork with no larger original. The model reads the
// normalised frame (internal/frame), so rotation, colour profile, bit depth
// and palette transparency are handled once. It synthesises detail,
// so imgkit.toml can forbid it. A project that forbids synthesis resamples
// plainly in its recipe instead.
package upscale

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	imgframe "github.com/lockyc/imgkit/internal/frame"
	"github.com/lockyc/imgkit/internal/ml"
	"github.com/lockyc/imgkit/internal/pins"
	"github.com/lockyc/imgkit/internal/policy"
)

const usage = "upscale <in> <out.png>"

// Main runs `imgkit upscale`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("upscale", usage, stderr)
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	if strings.ToLower(filepath.Ext(rest[1])) != ".png" {
		fmt.Fprintln(stderr, "imgkit upscale: the output must end .png")
		return 2
	}
	if err := run(ctx, rest[0], rest[1]); err != nil {
		return cli.Fail(stderr, "upscale", err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", rest[1])
	return 0
}

func run(ctx context.Context, in, out string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := policy.CheckSynthesis(wd, "upscale"); err != nil {
		return err
	}
	// The model reads a temporary frame, so no engine call names both <in>
	// and <out>; this is the one place that can refuse them being the same.
	if engine.SameFile(in, out) {
		return fmt.Errorf("%s is both an input and an output", in)
	}
	tmp, err := os.MkdirTemp("", "imgkit-upscale-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	frame := filepath.Join(tmp, "frame.png")
	if err := imgframe.Write(ctx, in, frame, imgframe.Options{}); err != nil {
		return err
	}
	args := []string{"--image", frame, "--out", out, "--model", pins.DAT.Repo, "--revision", pins.DAT.Revision, "--file", pins.DAT.File}
	_, err = ml.RunScript(ctx, "upscale.py", args, []string{frame}, []string{out})
	return err
}
