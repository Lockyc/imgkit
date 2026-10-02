package enginetest_test

import (
	"context"
	"slices"
	"testing"

	"github.com/lockyc/plate/internal/engine"
	"github.com/lockyc/plate/internal/enginetest"
)

func TestCallsRoundTripsAwkwardArguments(t *testing.T) {
	log := enginetest.Stub(t, "magick", `exit 0`)
	first := []string{"-o", "out.png", "--", "-rf"}
	second := []string{"line one\nline two", "", "--"}
	for _, args := range [][]string{first, second, nil} {
		if _, err := engine.Run(context.Background(), engine.Cmd{Engine: "magick", Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	calls := enginetest.Calls(t, log)
	want := [][]string{first, second, {}}
	if len(calls) != len(want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	for i := range want {
		if !slices.Equal(calls[i], want[i]) {
			t.Fatalf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}
