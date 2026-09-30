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

	"github.com/lockyc/imgkit/internal/doctor"
)

type command struct {
	name    string
	summary string
	main    func(ctx context.Context, args []string, stdout, stderr io.Writer) int
}

// commands is the dispatch table and the usage text, in display order.
var commands = []command{
	{"version", "print the imgkit version", versionMain},
	{"doctor", "check every engine; --install fetches the ones imgkit manages", doctor.Main},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func usage() string {
	var b strings.Builder
	b.WriteString("usage: imgkit <command> [flags] [args]\n\ncommands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-10s %s\n", c.name, c.summary)
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
