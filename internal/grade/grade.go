// Package grade moves colours, never pixels: fit recovers a
// reference's colour treatment as a HALD lookup table, and apply runs one
// through ImageMagick. apply reads the source in sRGB, as fit's frames are,
// so the table meets the values it was fitted on. -hald-clut on an image
// with alpha returns an opaque rectangle, so apply grades the colour alone
// and puts the alpha back.
package grade

import (
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/frame"
	"github.com/lockyc/imgkit/internal/icc"
)

const usage = "grade fit --ref ref.png [...] <subject.png> <out-hald.png>\n       imgkit grade apply --clut hald.png <in> <out.png>"

// Main dispatches `imgkit grade <subcommand>`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "fit":
			return fitMain(ctx, args[1:], stdout, stderr)
		case "apply":
			return applyMain(ctx, args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "usage: imgkit %s\n", usage)
	return 2
}

func applyMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("grade apply", "grade apply --clut hald.png <in> <out.png>", stderr)
	clut := fs.String("clut", "", "the HALD CLUT image (required)")
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	if *clut == "" || strings.ToLower(filepath.Ext(rest[1])) != ".png" {
		return cli.Usage(stderr, "grade apply", "give --clut, and an output ending .png")
	}
	if err := apply(ctx, rest[0], *clut, rest[1]); err != nil {
		return cli.Fail(stderr, "grade apply", err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", rest[1])
	return 0
}

func apply(ctx context.Context, in, clut, out string) error {
	res, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{"identify", "-format", "%[opaque] %w %h\n", in, clut}, Inputs: []string{in, clut}})
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	if len(lines) != 2 {
		return fmt.Errorf("could not read %s and %s: expected one frame per file (a multi-frame input is not supported)", in, clut)
	}
	opaque := strings.HasPrefix(lines[0], "True")
	cf := strings.Fields(lines[1])
	if len(cf) < 3 {
		return fmt.Errorf("could not read the size of %s", clut)
	}
	w, errW := strconv.Atoi(cf[1])
	h, errH := strconv.Atoi(cf[2])
	if errW != nil || errH != nil {
		return fmt.Errorf("could not read the size of %s", clut)
	}
	level := int(math.Round(math.Cbrt(float64(w))))
	if w != h || level*level*level != w {
		return fmt.Errorf("%s is %dx%d, not a HALD CLUT (a square of side level³, e.g. 512 for level 8)", clut, w, h)
	}
	srgb, err := icc.SRGB()
	if err != nil {
		return err
	}
	args := frame.SRGBArgs(in, srgb)
	if opaque {
		args = append(args, "-alpha", "off", clut, "-hald-clut", out)
	} else {
		args = append(args, "-write", "mpr:src", "-alpha", "off", clut, "-hald-clut",
			"(", "mpr:src", "-alpha", "extract", ")", "-alpha", "off", "-compose", "CopyOpacity", "-composite", out)
	}
	_, err = engine.Run(ctx, engine.Cmd{Engine: "magick", Args: args, Inputs: []string{in, clut}, Outputs: []string{out}})
	return err
}
