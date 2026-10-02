package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		args     []string
		code     int
		inStdout string
		inStderr string
	}{
		{nil, 2, "", "usage: plate"},
		{[]string{"version"}, 0, version, ""},
		{[]string{"help"}, 0, "usage: plate", ""},
		{[]string{"help"}, 0, "version", ""},
		{[]string{"nope"}, 2, "", `unknown command "nope"`},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if got := run(context.Background(), c.args, &out, &errb); got != c.code {
			t.Errorf("run(%q) = %d, want %d", c.args, got, c.code)
		}
		if !strings.Contains(out.String(), c.inStdout) {
			t.Errorf("run(%q) stdout %q lacks %q", c.args, out.String(), c.inStdout)
		}
		if !strings.Contains(errb.String(), c.inStderr) {
			t.Errorf("run(%q) stderr %q lacks %q", c.args, errb.String(), c.inStderr)
		}
	}
}

func TestEveryCommandIsInUsage(t *testing.T) {
	u := usage()
	for _, c := range commands {
		if !strings.Contains(u, "  "+c.name+" ") {
			t.Errorf("usage lacks %q", c.name)
		}
	}
}

func TestSynthesisPolicy(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile("plate.toml", []byte(`synthesis = "forbid"`), 0o644)
	for _, c := range commands {
		var out, errb bytes.Buffer
		code := run(context.Background(), []string{c.name, "-h"}, &out, &errb)
		forbidden := code == 1 && strings.Contains(errb.String(), "forbid")
		if forbidden != c.synthesises {
			t.Errorf("%s under synthesis = forbid: code %d, %q", c.name, code, errb.String())
		}
	}
	if !strings.Contains(usage(), "upscale    enlarge 4x with DAT (synthesises pixels)") {
		t.Errorf("usage does not mark the synthesising commands:\n%s", usage())
	}
}
