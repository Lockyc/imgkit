// Package enginetest stands in for external engines in tests: a POSIX sh
// script that plate finds through the engine's override variable.
package enginetest

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lockyc/plate/internal/engine"
)

// Stub makes a fake executable for engine name and points plate at it.
// body is POSIX sh and sees the real arguments as "$@". Before body runs,
// each invocation appends a record to the returned log: its argument count,
// then each argument, every field NUL-terminated. Arguments cannot contain
// NUL, so any argument — "--", an empty string, one spanning lines — reads
// back intact through Calls.
func Stub(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\000' \"$#\" \"$@\" >> '" + log + "'\n" +
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
	fields := bytes.Split(b, []byte{0})
	if len(fields[len(fields)-1]) != 0 {
		t.Fatalf("stub log %s: last field is not NUL-terminated", log)
	}
	fields = fields[:len(fields)-1]
	var calls [][]string
	for len(fields) > 0 {
		n, err := strconv.Atoi(string(fields[0]))
		if err != nil || n < 0 || n > len(fields)-1 {
			t.Fatalf("stub log %s: bad argument count %q", log, fields[0])
		}
		call := make([]string, n)
		for i := range call {
			call[i] = string(fields[1+i])
		}
		calls = append(calls, call)
		fields = fields[1+n:]
	}
	return calls
}
