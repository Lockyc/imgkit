// Command imgkit runs image and render operations for print and web
// pipelines. See README.md for the command set and docs/design.md for how it
// is built.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/cutout"
	"github.com/lockyc/imgkit/internal/diff"
	"github.com/lockyc/imgkit/internal/doctor"
	"github.com/lockyc/imgkit/internal/fonts"
	"github.com/lockyc/imgkit/internal/grade"
	"github.com/lockyc/imgkit/internal/infill"
	"github.com/lockyc/imgkit/internal/inpaint"
	"github.com/lockyc/imgkit/internal/policy"
	"github.com/lockyc/imgkit/internal/press"
	"github.com/lockyc/imgkit/internal/qr"
	"github.com/lockyc/imgkit/internal/render"
	"github.com/lockyc/imgkit/internal/upscale"
)

type command struct {
	name    string
	summary string
	main    func(ctx context.Context, args []string, stdout, stderr io.Writer) int
	// synthesises: the command creates pixels the source never had, so
	// imgkit.toml's synthesis policy is checked before it runs.
	synthesises bool
}

// commands is the dispatch table and the usage text, in display order.
var commands = []command{
	{"version", "print the imgkit version", versionMain, false},
	{"doctor", "check every engine; --install fetches the ones imgkit manages", doctor.Main, false},
	{"render", "HTML to PNG and/or PDF with headless Chrome", render.Main, false},
	{"press", "PDF to a print master: outlined text, CMYK, checked against a soft proof", press.Main, false},
	{"diff", "compare two images after lining them up vertically", diff.Main, false},
	{"fonts", "embed web fonts into a CSS file as data: URIs", fonts.Main, false},
	{"qr", "a QR code SVG, dark on light, quiet zone included", qr.Main, false},
	{"grade", "grade fit: fit a colour grade from a reference; apply it", grade.Main, false},
	{"cutout", "lift the subject out of an image as RGBA, down to hair and fur", cutout.Main, false},
	{"inpaint", "fill a masked region with LaMa", inpaint.Main, true},
	{"infill", "fill a hole in a flat ground from its surroundings", infill.Main, true},
	{"upscale", "enlarge 4x with DAT", upscale.Main, true},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// The first signal cancels; restoring the default lets a second one kill
	// work that does not watch ctx.
	go func() { <-ctx.Done(); stop() }()
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func usage() string {
	var b strings.Builder
	b.WriteString("usage: imgkit <command> [flags] [args]\n\ncommands:\n")
	for _, c := range commands {
		s := c.summary
		if c.synthesises {
			s += " (synthesises pixels)"
		}
		fmt.Fprintf(&b, "  %-10s %s\n", c.name, s)
	}
	b.WriteString("\nimgkit <command> -h prints a command's flags.\n")
	return b.String()
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage())
		return 0
	}
	for _, c := range commands {
		if c.name == args[0] {
			if c.synthesises {
				wd, err := os.Getwd()
				if err == nil {
					err = policy.CheckSynthesis(wd, c.name)
				}
				if err != nil {
					return cli.Fail(stderr, c.name, err)
				}
			}
			return c.main(ctx, args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "imgkit: unknown command %q\n\n%s", args[0], usage())
	return 2
}

func versionMain(_ context.Context, _ []string, stdout, _ io.Writer) int {
	fmt.Fprintln(stdout, version)
	return 0
}
