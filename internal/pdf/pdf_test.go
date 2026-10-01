package pdf

import (
	"context"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
)

func TestRead(t *testing.T) {
	enginetest.Stub(t, "pdfinfo", `printf 'Title:          x\nPages:          3\nPage size:      595.276 x 841.89 pts (A4)\n'`)
	info, err := Read(context.Background(), "in.pdf")
	if err != nil || info != (Info{Pages: 3, W: 595.276, H: 841.89}) {
		t.Fatalf("Read = %+v, %v", info, err)
	}
}

func TestPageSizes(t *testing.T) {
	log := enginetest.Stub(t, "pdfinfo", `printf 'Page    1 size: 900 x 675 pts\nPage    2 size: 612 x 792 pts (letter)\n'`)
	got, err := PageSizes(context.Background(), "in.pdf", 2)
	if err != nil || len(got) != 2 || got[1] != [2]float64{612, 792} {
		t.Fatalf("PageSizes = %v, %v", got, err)
	}
	if c := enginetest.Calls(t, log)[0]; c[0] != "-f" || c[1] != "1" || c[2] != "-l" || c[3] != "2" {
		t.Errorf("args %q", c)
	}
}

func TestReadNoBox(t *testing.T) {
	enginetest.Stub(t, "pdfinfo", `printf 'Pages: 1\n'`)
	if _, err := Read(context.Background(), "in.pdf"); err == nil {
		t.Fatal("missing page size accepted")
	}
}
