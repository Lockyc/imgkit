// Package inpaint fills a masked region with LaMa (iopaint). It synthesises
// pixels, so imgkit.toml can forbid it. LaMa needs the same kind of texture
// around a hole: next to a large flat area it fills flat grey. A hole in a
// flat ground is infill's job.
package inpaint

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/imgsize"
	"github.com/lockyc/imgkit/internal/ml"
	"github.com/lockyc/imgkit/internal/pins"
	"github.com/lockyc/imgkit/internal/policy"
)

const usage = "inpaint --mask mask.png [--device cpu|mps] <in> <out.png>"

// Main runs `imgkit inpaint`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("inpaint", usage, stderr)
	mask := fs.String("mask", "", "white where LaMa fills, same size as the image (required)")
	device := fs.String("device", "cpu", "cpu or mps")
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	if *mask == "" || strings.ToLower(filepath.Ext(rest[1])) != ".png" || (*device != "cpu" && *device != "mps") {
		fmt.Fprintln(stderr, "imgkit inpaint: give --mask, --device cpu|mps, and an output ending .png")
		return 2
	}
	if err := run(ctx, rest[0], *mask, rest[1], *device); err != nil {
		return cli.Fail(stderr, "inpaint", err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", rest[1])
	return 0
}

func run(ctx context.Context, in, mask, out, device string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := policy.CheckSynthesis(wd, "inpaint"); err != nil {
		return err
	}
	// The step that writes <out> reads a temp file, so no engine call names
	// both <in> or <mask> and <out>; this is the one place that can refuse
	// before the temp work runs.
	if engine.SameFile(in, out) || engine.SameFile(mask, out) {
		return fmt.Errorf("%s is also an input; write the result elsewhere", out)
	}
	iw, ih, err := imgsize.Dims(in)
	if err != nil {
		return err
	}
	mw, mh, err := imgsize.Dims(mask)
	if err != nil {
		return err
	}
	if iw != mw || ih != mh {
		return fmt.Errorf("the mask is %dx%d and the image %dx%d; iopaint would resize the mask silently", mw, mh, iw, ih)
	}
	tmp, err := os.MkdirTemp("", "imgkit-inpaint-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// A PNG copy under a fixed name, so iopaint's output name is known.
	src, outDir := filepath.Join(tmp, "in.png"), filepath.Join(tmp, "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		return err
	}
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{in, src}, Inputs: []string{in}, Outputs: []string{src}}); err != nil {
		return err
	}
	painted := filepath.Join(outDir, "in.png")
	if _, err := ml.RunTool(ctx, pins.IOPaint, "iopaint", []string{"run", "--model=lama", "--device=" + device,
		"--image=" + src, "--mask=" + mask, "--output=" + outDir}, []string{src, mask}, []string{painted}); err != nil {
		return err
	}
	_, err = engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{painted, out}, Inputs: []string{painted}, Outputs: []string{out}})
	return err
}
