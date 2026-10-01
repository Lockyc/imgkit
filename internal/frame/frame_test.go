package frame

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
)

func TestArgsOrder(t *testing.T) {
	for _, c := range []struct {
		o    Options
		want []string
	}{
		{Options{}, []string{"in.jpg", "-auto-orient", "-profile", "s.icc", "-strip", "-depth", "8", "PNG32:o.png"}},
		{Options{Height: 600, Opaque: true}, []string{"in.jpg", "-auto-orient", "-profile", "s.icc", "-strip", "-resize", "x600", "-alpha", "off", "-depth", "8", "PNG24:o.png"}},
	} {
		if got := Args("in.jpg", "o.png", "s.icc", c.o); !slices.Equal(got, c.want) {
			t.Errorf("%+v: args %q, want %q", c.o, got, c.want)
		}
	}
}

func TestWrite(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	log := enginetest.Stub(t, "magick", `for a in "$@"; do last=$a; done; printf png > "${last#PNG32:}"`)
	out := t.TempDir() + "/o.png"
	if err := Write(context.Background(), "in.jpg", out, Options{}); err != nil {
		t.Fatal(err)
	}
	c := enginetest.Calls(t, log)[0]
	if c[0] != "in.jpg" || !strings.HasSuffix(c[3], ".icc") || c[len(c)-1] != "PNG32:"+out {
		t.Errorf("call %q", c)
	}
}
