// Package pdf reads PDFs through poppler: the page count, page boxes and
// text other commands need, and `plate pdf`, which inspects a PDF (info
// with a text-layer check, text, page renders, embedded images).
package pdf

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/lockyc/plate/internal/engine"
)

// Info is a PDF's page count and page 1's box, in points.
type Info struct {
	Pages int
	W, H  float64
}

var (
	pagesRe   = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
	boxRe     = regexp.MustCompile(`(?m)^Page size:\s+([0-9.]+) x ([0-9.]+) pts`)
	pageBoxRe = regexp.MustCompile(`(?m)^Page\s+\d+ size:\s+([0-9.]+) x ([0-9.]+) pts`)
)

// Read runs pdfinfo on path.
func Read(ctx context.Context, path string) (Info, error) {
	res, err := engine.Run(ctx, engine.Cmd{Engine: "pdfinfo", Args: []string{path}, Inputs: []string{path}})
	if err != nil {
		return Info{}, err
	}
	p := pagesRe.FindSubmatch(res.Stdout)
	b := boxRe.FindSubmatch(res.Stdout)
	if p == nil || b == nil {
		return Info{}, fmt.Errorf("%s: pdfinfo reported no page count or page size", path)
	}
	n, errN := strconv.Atoi(string(p[1]))
	w, errW := strconv.ParseFloat(string(b[1]), 64)
	h, errH := strconv.ParseFloat(string(b[2]), 64)
	if errN != nil || errW != nil || errH != nil || n < 1 {
		return Info{}, fmt.Errorf("%s: pdfinfo reported an unusable page count or size", path)
	}
	return Info{Pages: n, W: w, H: h}, nil
}

// PageSizes reads every page's box.
func PageSizes(ctx context.Context, path string, pages int) ([][2]float64, error) {
	if pages < 1 {
		return nil, fmt.Errorf("%s: page count %d, want at least 1", path, pages)
	}
	res, err := engine.Run(ctx, engine.Cmd{Engine: "pdfinfo", Args: []string{"-f", "1", "-l", strconv.Itoa(pages), path}, Inputs: []string{path}})
	if err != nil {
		return nil, err
	}
	var out [][2]float64
	for _, m := range pageBoxRe.FindAllSubmatch(res.Stdout, -1) {
		w, errW := strconv.ParseFloat(string(m[1]), 64)
		h, errH := strconv.ParseFloat(string(m[2]), 64)
		if errW != nil || errH != nil {
			return nil, fmt.Errorf("%s: pdfinfo listed an unusable page size %q", path, m[0])
		}
		out = append(out, [2]float64{w, h})
	}
	if len(out) != pages {
		return nil, fmt.Errorf("%s: pdfinfo listed %d page sizes, want %d", path, len(out), pages)
	}
	return out, nil
}

// Text extracts path's text with pdftotext.
func Text(ctx context.Context, path string) (string, error) {
	res, err := engine.Run(ctx, engine.Cmd{Engine: "pdftotext", Args: []string{path, "-"}, Inputs: []string{path}})
	return string(res.Stdout), err
}
