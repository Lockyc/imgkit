package qr

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
)

func TestQR(t *testing.T) {
	log := enginetest.Stub(t, "qrencode", `while [ "$1" != "-o" ]; do shift; done; printf '<svg/>' > "$2"`)
	out := filepath.Join(t.TempDir(), "qr.svg")
	var o, e bytes.Buffer
	if code := Main(context.Background(), []string{"--fg", "062D5F", "-o", out, "--", "-rf"}, &o, &e); code != 0 {
		t.Fatalf("code %d: %s", code, e.String())
	}
	got := enginetest.Calls(t, log)[0]
	want := []string{"-t", "SVG", "-l", "M", "-m", "4", "-s", "1", "--foreground=062D5F", "--background=FFFFFF", "-o", out, "--", "-rf"}
	if !slices.Equal(got, want) {
		t.Errorf("args %q\nwant %q", got, want)
	}
}

func TestQRRefuses(t *testing.T) {
	enginetest.Stub(t, "qrencode", `exit 0`)
	out := filepath.Join(t.TempDir(), "qr.svg")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--fg", "FFFFFF", "--bg", "062D5F", "-o", out, "x"}, "light on dark"},
		{[]string{"--fg", "zz0000", "-o", out, "x"}, "--fg"},
		{[]string{"--ec", "X", "-o", out, "x"}, "--ec"},
		{[]string{"x"}, "-o"},
	}
	for _, c := range cases {
		var o, e bytes.Buffer
		if code := Main(context.Background(), c.args, &o, &e); code != 2 || !strings.Contains(e.String(), c.want) {
			t.Errorf("%q: code %d stderr %q", c.args, code, e.String())
		}
	}
}

func TestQRValidatesFgBeforeBg(t *testing.T) {
	enginetest.Stub(t, "qrencode", `exit 0`)
	out := filepath.Join(t.TempDir(), "qr.svg")
	for i := 0; i < 20; i++ {
		var o, e bytes.Buffer
		if code := Main(context.Background(), []string{"--fg", "zz", "--bg", "yy", "-o", out, "x"}, &o, &e); code != 2 || !strings.Contains(e.String(), "--fg") {
			t.Fatalf("code %d stderr %q", code, e.String())
		}
	}
}
