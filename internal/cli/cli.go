// Package cli holds the flag and error conventions every imgkit command
// shares: flags before positional arguments, exit 2 for a usage error, exit
// 1 for a failed operation, errors printed as "imgkit <op>: <reason>".
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// Flags makes the flag set for "imgkit <op>".
func Flags(op, usage string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("imgkit "+op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: imgkit %s\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// Parse parses args and checks the positional count (any count when
// positional < 0). When ok is false the caller returns code.
func Parse(fs *flag.FlagSet, args []string, positional int) (rest []string, code int, ok bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, 0, false
		}
		return nil, 2, false
	}
	if positional >= 0 && fs.NArg() != positional {
		fs.Usage()
		return nil, 2, false
	}
	return fs.Args(), 0, true
}

// Usage reports a usage error for op and returns exit status 2.
func Usage(stderr io.Writer, op, format string, a ...any) int {
	fmt.Fprintf(stderr, "imgkit %s: %s\n", op, fmt.Sprintf(format, a...))
	return 2
}

// Fail reports err for op and returns exit status 1.
func Fail(stderr io.Writer, op string, err error) int {
	fmt.Fprintf(stderr, "imgkit %s: %v\n", op, err)
	return 1
}
