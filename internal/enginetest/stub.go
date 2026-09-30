// Package enginetest stands in for external engines in tests: a POSIX sh
// script that imgkit finds through the engine's override variable.
package enginetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/engine"
)

// Stub makes a fake executable for engine name and points imgkit at it.
// body is POSIX sh and sees the real arguments as "$@". Before body runs,
// each invocation appends its arguments to the returned log, one per line,
// followed by a line holding only "--".
func Stub(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + log + "'; done\n" +
		"printf -- '--\\n' >> '" + log + "'\n" +
		body + "\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(engine.EnvOverride(name), path)
	return log
}

// Calls reads a stub's log: one slice of arguments per invocation, in order.
// A stub that never ran has no log, which reads as no calls.
func Calls(t *testing.T, log string) [][]string {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	cur := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if line == "--" {
			calls = append(calls, cur)
			cur = []string{}
			continue
		}
		cur = append(cur, line)
	}
	return calls
}
