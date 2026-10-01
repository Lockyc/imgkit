// Package cutout lifts the subject out of an image as RGBA at the source's
// framing: a coarse mask (Apple Vision or BiRefNet) as the prior, ViTMatte
// solving the edge at full resolution in tiles, foreground estimation to
// lift the background's tint out of soft edges, and an optional despill.
// Every step computes alpha or un-mixes captured colour, so cutout is not
// a synthesising command. The output is PNG: a soft matte edge is exactly
// the artefact print cannot hide, and lossy alpha damages it.
package cutout

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/icc"
	"github.com/lockyc/imgkit/internal/ml"
	"github.com/lockyc/imgkit/internal/pins"
	"github.com/lockyc/imgkit/internal/raster"
	"github.com/lockyc/imgkit/internal/vision"
)

const usage = "cutout [--coarse vision|birefnet] [--height PX] [--tile PX] [--band-in D] [--band-out D] [--despill PRESET] [--despill-hue A:B] [--despill-clean A:B] [--despill-chroma A:B] [--despill-lmax L] [--despill-hue-end H] <in> <out.png>"

// despill is one named set of despill values.
type despill struct{ hue, clean, chroma, lmax, hueEnd string }

// presets name despill settings measured on a real subject. warm-on-green
// is warm hair and skin (Lab hue 15–65°) over foliage: contamination ramps
// in from 70° to 100°, stops at 200° (blue eyes and sky are not spill), at
// chroma 6–40 and lightness below 88.
var presets = map[string]despill{
	"warm-on-green": {hue: "70:100", clean: "15:65", chroma: "6:40", lmax: "88", hueEnd: "200"},
}

func defaultCoarse() string {
	if runtime.GOOS == "darwin" {
		return "vision"
	}
	return "birefnet"
}

func isPair(s string) bool {
	a, b, ok := strings.Cut(s, ":")
	_, e1 := strconv.ParseFloat(a, 64)
	_, e2 := strconv.ParseFloat(b, 64)
	return ok && e1 == nil && e2 == nil
}

type job struct {
	in, out, coarse         string
	height, tile, bin, bout int
	despill                 despill
}

// Main runs `imgkit cutout`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("cutout", usage, stderr)
	coarse := fs.String("coarse", defaultCoarse(), "coarse mask: vision (macOS) or birefnet")
	height := fs.Int("height", 0, "working height in px, never above the source; at least the largest size the cut-out will be drawn at (0 = source height)")
	tile := fs.Int("tile", 1024, "ViTMatte tile size in px; 0 runs the whole frame (memory grows with the fourth power of height)")
	bin := fs.Int("band-in", 60, "sure foreground: the coarse mask eroded by height/D")
	bout := fs.Int("band-out", 40, "sure background: beyond the coarse mask dilated by height/D")
	preset := fs.String("despill", "", "despill preset: warm-on-green")
	hue := fs.String("despill-hue", "", "contamination ramp in Lab hue degrees, A:B (enables despill)")
	clean := fs.String("despill-clean", "", "hue range of clean subject pixels, A:B")
	chroma := fs.String("despill-chroma", "", "chroma range treated as spill, A:B")
	lmax := fs.String("despill-lmax", "", "only pixels below this Lab lightness")
	hueEnd := fs.String("despill-hue-end", "", "hue above which a pixel is not spill")
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	usageErr := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "imgkit cutout: "+format+"\n", a...)
		return 2
	}
	j := job{in: rest[0], out: rest[1], coarse: *coarse, height: *height, tile: *tile, bin: *bin, bout: *bout}
	if strings.ToLower(filepath.Ext(j.out)) != ".png" {
		return usageErr("the output must be .png: lossy alpha damages the matte edge")
	}
	if j.coarse != "vision" && j.coarse != "birefnet" {
		return usageErr("--coarse %q: use vision or birefnet", j.coarse)
	}
	if *preset != "" {
		p, ok := presets[*preset]
		if !ok {
			return usageErr("--despill %q: known presets are warm-on-green", *preset)
		}
		j.despill = p
	}
	for _, o := range []struct {
		v    string
		dst  *string
		pair bool
	}{{*hue, &j.despill.hue, true}, {*clean, &j.despill.clean, true}, {*chroma, &j.despill.chroma, true}, {*lmax, &j.despill.lmax, false}, {*hueEnd, &j.despill.hueEnd, false}} {
		if o.v == "" {
			continue
		}
		if o.pair && !isPair(o.v) {
			return usageErr("%q: want A:B", o.v)
		}
		*o.dst = o.v
	}
	if j.coarse == "vision" {
		if err := vision.Available(runtime.GOOS); err != nil {
			return cli.Fail(stderr, "cutout", err)
		}
	}
	w, h, err := j.run(ctx)
	if err != nil {
		return cli.Fail(stderr, "cutout", err)
	}
	fmt.Fprintf(stdout, "wrote %s (%dx%d)\n", j.out, w, h)
	return 0
}

func (j job) run(ctx context.Context) (int, int, error) {
	// The matte step writes <out> after temp files and two engines have run,
	// and none of those steps names both <in> and <out>, so this is the one
	// place that can refuse them being the same file before any work.
	if engine.SameFile(j.in, j.out) {
		return 0, 0, fmt.Errorf("%s is both the input and the output", j.in)
	}
	res, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{j.in, "-auto-orient", "-format", "%wx%h", "info:"}, Inputs: []string{j.in}})
	if err != nil {
		return 0, 0, err
	}
	var sw, sh int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(res.Stdout)), "%dx%d", &sw, &sh); err != nil {
		return 0, 0, fmt.Errorf("could not read the size of %s", j.in)
	}
	h := j.height
	if h == 0 {
		h = sh
	}
	if h > sh {
		return 0, 0, fmt.Errorf("--height %d would upsample the %d px source; a cut-out is never upsampled", h, sh)
	}
	srgb, err := icc.SRGB()
	if err != nil {
		return 0, 0, err
	}
	tmp, err := os.MkdirTemp("", "imgkit-cutout-")
	if err != nil {
		return 0, 0, err
	}
	defer os.RemoveAll(tmp)
	frame, mask := filepath.Join(tmp, "frame.png"), filepath.Join(tmp, "coarse.png")
	// Orient, then convert to sRGB, then strip: stripping first loses the
	// rotation and leaves a wide-gamut photo plausibly duller.
	frameArgs := []string{j.in, "-auto-orient", "-profile", srgb, "-strip", "-resize", "x" + strconv.Itoa(h), "-alpha", "off", "-depth", "8", frame}
	if _, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: frameArgs, Inputs: []string{j.in}, Outputs: []string{frame}}); err != nil {
		return 0, 0, err
	}
	if err := j.coarseMask(ctx, frame, mask, tmp); err != nil {
		return 0, 0, err
	}
	if err := hasForeground(mask); err != nil {
		return 0, 0, fmt.Errorf("%s: %w", j.in, err)
	}
	args := []string{"--image", frame, "--coarse", mask, "--out", j.out,
		"--model", pins.ViTMatte.Repo, "--revision", pins.ViTMatte.Revision,
		"--band-in", strconv.Itoa(j.bin), "--band-out", strconv.Itoa(j.bout),
		"--tile", strconv.Itoa(j.tile), "--overlap", "128"}
	if d := j.despill; d.hue != "" {
		args = append(args, "--despill-hue", d.hue)
		for _, kv := range [][2]string{{"--despill-clean", d.clean}, {"--despill-chroma", d.chroma}, {"--despill-lmax", d.lmax}, {"--despill-hue-end", d.hueEnd}} {
			if kv[1] != "" {
				args = append(args, kv[0], kv[1])
			}
		}
	}
	if _, err := ml.RunScript(ctx, "matte.py", args, []string{frame, mask}, []string{j.out}); err != nil {
		return 0, 0, err
	}
	out, err := raster.Load(j.out)
	if err != nil {
		os.Remove(j.out)
		return 0, 0, err
	}
	return out.Bounds().Dx(), out.Bounds().Dy(), nil
}

func (j job) coarseMask(ctx context.Context, frame, mask, tmp string) error {
	if j.coarse == "birefnet" {
		_, err := ml.RunScript(ctx, "birefnet.py", []string{frame, mask}, []string{frame}, []string{mask})
		return err
	}
	rgba := filepath.Join(tmp, "coarse-rgba.png")
	if err := vision.Mask(ctx, frame, rgba); err != nil {
		return err
	}
	_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: []string{rgba, "-alpha", "extract", mask}, Inputs: []string{rgba}, Outputs: []string{mask}})
	return err
}

func hasForeground(mask string) error {
	m, err := raster.Load(mask)
	if err != nil {
		return err
	}
	b := m.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if m.RGBA64At(x, y).R > 0x7fff {
				return nil
			}
		}
	}
	return fmt.Errorf("no foreground found")
}
