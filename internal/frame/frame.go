// Package frame normalises a source image before an operation reads its
// pixels: EXIF rotation applied, colour converted to sRGB, 8 bits per
// channel, RGB(A) whatever the source's PNG colour type. A 16-bit grey,
// palette or wide-gamut source then reads the same to every model. Orient
// applies the rotation alone, for an operation whose output keeps the
// source's depth and profile.
package frame

import (
	"context"
	"strconv"

	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/icc"
)

// Options shape the frame.
type Options struct {
	Height int  // resize to this height; 0 keeps the source's
	Opaque bool // drop alpha and write RGB; otherwise RGBA, opaque where the source has no alpha
}

// Args is the magick command line that writes in's frame to out. It orients,
// then converts to sRGB, then strips: stripping first loses the rotation and
// leaves a wide-gamut photo plausibly duller. The PNG24/PNG32 prefix forces a
// colour type, since magick otherwise keeps a grey or palette source as grey
// or palette.
func Args(in, out, srgb string, o Options) []string {
	a := append(oriented(in), "-profile", srgb, "-strip")
	if o.Height > 0 {
		a = append(a, "-resize", "x"+strconv.Itoa(o.Height))
	}
	format := "PNG32:"
	if o.Opaque {
		a = append(a, "-alpha", "off")
		format = "PNG24:"
	}
	return append(a, "-depth", "8", format+out)
}

// Write writes in's frame to out, a PNG.
func Write(ctx context.Context, in, out string, o Options) error {
	srgb, err := icc.SRGB()
	if err != nil {
		return err
	}
	_, err = engine.Run(ctx, engine.Cmd{Engine: "magick", Args: Args(in, out, srgb, o), Inputs: []string{in}, Outputs: []string{out}})
	return err
}

// oriented reads in as it is displayed: its EXIF rotation applied.
func oriented(in string) []string { return []string{in, "-auto-orient"} }

// OrientArgs is the magick command line that writes in to out rotated as
// displayed and otherwise as stored: bit depth, colour profile and colour
// type stay the source's.
func OrientArgs(in, out string) []string { return append(oriented(in), out) }

// Orient writes in to out, a PNG, with only its EXIF rotation applied, so a
// mask drawn on the displayed image lines up with it.
func Orient(ctx context.Context, in, out string) error {
	_, err := engine.Run(ctx, engine.Cmd{Engine: "magick", Args: OrientArgs(in, out), Inputs: []string{in}, Outputs: []string{out}})
	return err
}
