// Command imgkit runs image and render operations for print and web
// pipelines. See README.md for the command set and docs/design.md for how it
// is built.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: imgkit <command> [args]

commands:
  version    print the imgkit version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "imgkit: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
