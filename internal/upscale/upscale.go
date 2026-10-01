// Package upscale enlarges an image 4× with Real-ESRGAN (realesrgan-x4plus),
// for artwork with no larger original. It synthesises detail, so
// imgkit.toml can forbid it. A project that forbids synthesis resamples
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
	"github.com/lockyc/imgkit/internal/policy"
)

const usage = "upscale <in> <out.png>"

const esrgan = "realesrgan-ncnn-vulkan"

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
	bin, err := engine.Resolve(esrgan)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return err
	}
	// The release ships its models beside the binary.
	models := filepath.Join(filepath.Dir(real), "models")
	_, err = engine.Run(ctx, engine.Cmd{Engine: esrgan, Args: []string{"-i", in, "-o", out, "-n", "realesrgan-x4plus", "-m", models}, Inputs: []string{in}, Outputs: []string{out}})
	return err
}
