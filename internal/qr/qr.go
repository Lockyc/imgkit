// Package qr makes a QR code SVG with qrencode: dark modules on a light
// ground, with the four-module quiet zone inside the file. Every reader
// decodes dark on light, not every one decodes the inverse, so an inverted
// pair is refused rather than trusted to the reader.
package qr

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lockyc/plate/internal/cli"
	"github.com/lockyc/plate/internal/engine"
)

const usage = "qr [--ec L|M|Q|H] [--fg RRGGBB] [--bg RRGGBB] -o out.svg <text>"

var hexRe = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

func luminance(hex string) float64 {
	lin := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(0) + 0.7152*lin(2) + 0.0722*lin(4)
}

// Main runs `plate qr`.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("qr", usage, stderr)
	ec := fs.String("ec", "M", "error correction: L, M, Q or H")
	fg := fs.String("fg", "000000", "module colour, RRGGBB")
	bg := fs.String("bg", "FFFFFF", "ground colour, RRGGBB")
	out := fs.String("o", "", "the SVG to write (required)")
	rest, code, ok := cli.Parse(fs, args, 1)
	if !ok {
		return code
	}
	usageErr := func(format string, a ...any) int { return cli.Usage(stderr, "qr", format, a...) }
	if *out == "" {
		return usageErr("-o is required")
	}
	if !strings.Contains("LMQH", *ec) || len(*ec) != 1 {
		return usageErr("--ec %q: use L, M, Q or H", *ec)
	}
	for _, c := range []struct{ name, v string }{{"--fg", *fg}, {"--bg", *bg}} {
		if !hexRe.MatchString(c.v) {
			return usageErr("%s %q: want RRGGBB", c.name, c.v)
		}
	}
	f, b := strings.ToUpper(*fg), strings.ToUpper(*bg)
	if luminance(f) >= luminance(b) {
		return usageErr("the code would be light on dark: --fg %s is not darker than --bg %s", f, b)
	}
	cmd := engine.Cmd{Engine: "qrencode", Outputs: []string{*out}, Args: []string{
		"-t", "SVG", "-l", *ec, "-m", "4", "-s", "1",
		"--foreground=" + f, "--background=" + b, "-o", *out, "--", rest[0]}}
	if _, err := engine.Run(ctx, cmd); err != nil {
		return cli.Fail(stderr, "qr", err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", *out)
	return 0
}
