// Package infill fills holes in a flat ground (paper, a painted wall, sky)
// from their surroundings: each level blurs the image with its holes
// transparent, keeps what the blur reached, and the levels stack from
// coarse to fine, so a hole takes the colour of what is nearest. A faint
// seeded Gaussian grain then keeps the patch from being flatter than the
// ground around it. It synthesises pixels, so imgkit.toml can forbid it.
package infill

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/imgsize"
	"github.com/lockyc/imgkit/internal/policy"
)

const usage = "infill --mask mask.png [--levels 150,60,20,6] [--grain 0.25] [--seed 1] <in> <out.png>"

// Main runs `imgkit infill`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("infill", usage, stderr)
	mask := fs.String("mask", "", "white where the holes are, same size as the image (required)")
	levels := fs.String("levels", "150,60,20,6", "blur radii in px, coarse to fine")
	grain := fs.Float64("grain", 0.25, "Gaussian grain strength (0 for none)")
	seed := fs.Int("seed", 1, "grain seed")
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	var radii []int
	for _, s := range strings.Split(*levels, ",") {
		r, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || r <= 0 || (len(radii) > 0 && r >= radii[len(radii)-1]) {
			fmt.Fprintln(stderr, "imgkit infill: --levels must be positive radii, strictly coarse to fine")
			return 2
		}
		radii = append(radii, r)
	}
	if *mask == "" || strings.ToLower(filepath.Ext(rest[1])) != ".png" || *grain < 0 {
		fmt.Fprintln(stderr, "imgkit infill: give --mask, a --grain of 0 or more, and an output ending .png")
		return 2
	}
	if err := run(ctx, rest[0], *mask, rest[1], radii, *grain, *seed); err != nil {
		return cli.Fail(stderr, "infill", err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", rest[1])
	return 0
}

func run(ctx context.Context, in, mask, out string, radii []int, grain float64, seed int) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := policy.CheckSynthesis(wd, "infill"); err != nil {
		return err
	}
	// The step that writes <out> reads temp files, so no engine call names
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
		return fmt.Errorf("the mask is %dx%d and the image %dx%d", mw, mh, iw, ih)
	}
	tmp, err := os.MkdirTemp("", "imgkit-infill-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	magick := func(inputs []string, args ...string) error {
		_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: args, Inputs: inputs, Outputs: []string{args[len(args)-1]}})
		return err
	}
	holes := filepath.Join(tmp, "holes.png")
	if err := magick([]string{in, mask}, in, "(", mask, "-negate", ")", "-alpha", "off", "-compose", "CopyOpacity", "-composite", holes); err != nil {
		return err
	}
	var layers []string
	for _, r := range radii {
		l := filepath.Join(tmp, fmt.Sprintf("b%d.png", r))
		if err := magick([]string{holes}, holes, "-channel", "RGBA", "-blur", fmt.Sprintf("0x%d", r), "-channel", "A", "-threshold", "0.1%", "+channel", l); err != nil {
			return err
		}
		layers = append(layers, l)
	}
	fill := filepath.Join(tmp, "fill.png")
	args := []string{layers[0]}
	for _, l := range layers[1:] {
		args = append(args, l, "-compose", "over", "-composite")
	}
	args = append(args, "-alpha", "off")
	if grain > 0 {
		args = append(args, "-seed", strconv.Itoa(seed), "-attenuate", strconv.FormatFloat(grain, 'f', -1, 64), "+noise", "Gaussian")
	}
	if err := magick(layers, append(args, fill)...); err != nil {
		return err
	}
	return magick([]string{fill, holes}, fill, holes, "-compose", "over", "-composite", out)
}
