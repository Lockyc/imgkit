package main

import (
	"bytes"
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
		{nil, 2, "", "usage: imgkit"},
		{[]string{"version"}, 0, version, ""},
		{[]string{"help"}, 0, "usage: imgkit", ""},
		{[]string{"nope"}, 2, "", `unknown command "nope"`},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.code {
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
