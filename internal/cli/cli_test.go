package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	var errb bytes.Buffer
	fs := Flags("demo", "demo [--n N] <in> <out>", &errb)
	n := fs.Int("n", 1, "a number")
	rest, code, ok := Parse(fs, []string{"--n", "3", "a", "b"}, 2)
	if !ok || code != 0 || *n != 3 || len(rest) != 2 {
		t.Fatalf("Parse = %q %d %v, n=%d", rest, code, ok, *n)
	}
	errb.Reset()
	if _, code, ok := Parse(Flags("demo", "demo <in>", &errb), []string{"a", "b"}, 1); ok || code != 2 {
		t.Fatalf("wrong positional count: code %d ok %v", code, ok)
	}
	if !strings.Contains(errb.String(), "usage: imgkit demo <in>") {
		t.Errorf("usage not printed: %q", errb.String())
	}
	if _, code, ok := Parse(Flags("demo", "demo", &errb), []string{"-h"}, 0); ok || code != 0 {
		t.Fatalf("-h: code %d ok %v", code, ok)
	}
	if _, code, ok := Parse(Flags("demo", "demo", &errb), []string{"--bogus"}, 0); ok || code != 2 {
		t.Fatalf("unknown flag: code %d ok %v", code, ok)
	}
}

func TestFail(t *testing.T) {
	var errb bytes.Buffer
	if code := Fail(&errb, "render", errors.New("render would be 300.0 MP")); code != 1 {
		t.Fatalf("code %d", code)
	}
	if errb.String() != "imgkit render: render would be 300.0 MP\n" {
		t.Errorf("stderr %q", errb.String())
	}
}

func TestFloatRefusesNonFinite(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-Inf"} {
		var errb bytes.Buffer
		fs := Flags("demo", "demo", &errb)
		Float(fs, "x", 1, "a number")
		if _, code, ok := Parse(fs, []string{"--x", v}, 0); ok || code != 2 {
			t.Errorf("--x %s: code %d ok %v", v, code, ok)
		}
	}
	fs := Flags("demo", "demo", &bytes.Buffer{})
	x := Float(fs, "x", 1, "a number")
	if _, _, ok := Parse(fs, []string{"--x", "2.5"}, 0); !ok || *x != 2.5 {
		t.Fatalf("--x 2.5: ok %v x %v", ok, *x)
	}
}
