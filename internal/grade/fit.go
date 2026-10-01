package grade

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/lockyc/imgkit/internal/cli"
	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/frame"
	"github.com/lockyc/imgkit/internal/ml"
)

var rectRe = regexp.MustCompile(`^\d+,\d+,\d+,\d+$`)

type rects []string

func (r *rects) String() string { return fmt.Sprint(*r) }
func (r *rects) Set(s string) error {
	if !rectRe.MatchString(s) {
		return fmt.Errorf("%q: want x,y,w,h in pixels", s)
	}
	*r = append(*r, s)
	return nil
}

func fitMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.Flags("grade fit", "grade fit --ref ref.png [--ref-crop x,y,w,h] [--exclude x,y,w,h]... [--level 8] [--min-inliers 40] <subject.png> <out-hald.png>", stderr)
	ref := fs.String("ref", "", "the reference image carrying the treatment (required)")
	var crop, exclude rects
	fs.Var(&crop, "ref-crop", "the subject's box in the reference, x,y,w,h px")
	fs.Var(&exclude, "exclude", "a box inside the crop drawn over the subject, left out of the fit (repeatable)")
	level := fs.Int("level", 8, "HALD level: level³ px square, level² steps per channel")
	minInliers := fs.Int("min-inliers", 40, "RANSAC inliers needed to trust the registration")
	rest, code, ok := cli.Parse(fs, args, 2)
	if !ok {
		return code
	}
	if *ref == "" || len(crop) > 1 || *level < 2 || *level > 16 {
		fmt.Fprintln(stderr, "imgkit grade fit: give --ref, at most one --ref-crop, and a --level of 2-16")
		return 2
	}
	var extra []string
	if len(crop) == 1 {
		extra = append(extra, "--ref-crop", crop[0])
	}
	for _, e := range exclude {
		extra = append(extra, "--exclude", e)
	}
	extra = append(extra, "--level", strconv.Itoa(*level), "--min-inliers", strconv.Itoa(*minInliers))
	res, err := fit(ctx, rest[0], *ref, rest[1], extra)
	if err != nil {
		return cli.Fail(stderr, "grade fit", err)
	}
	stdout.Write(res)
	return 0
}

// fit fits on the subject's and reference's normalised frames, so
// --ref-crop is in the reference's displayed orientation and the lookup maps
// sRGB to sRGB rather than across two gamuts. The subject keeps its alpha,
// which marks the pixels the fit may use.
func fit(ctx context.Context, subject, ref, out string, extra []string) ([]byte, error) {
	// The script reads temporary frames, so no engine call names both an
	// input and <out>; this is the one place that can refuse them being
	// the same before any work.
	if engine.SameFile(subject, out) || engine.SameFile(ref, out) {
		return nil, fmt.Errorf("%s is also an input; write the result elsewhere", out)
	}
	tmp, err := os.MkdirTemp("", "imgkit-gradefit-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	s, r := filepath.Join(tmp, "subject.png"), filepath.Join(tmp, "ref.png")
	if err := frame.Write(ctx, subject, s, frame.Options{}); err != nil {
		return nil, err
	}
	if err := frame.Write(ctx, ref, r, frame.Options{Opaque: true}); err != nil {
		return nil, err
	}
	a := append([]string{"--subject", s, "--ref", r, "--out", out}, extra...)
	res, err := ml.RunScript(ctx, "gradefit.py", a, []string{s, r}, []string{out})
	return res.Stdout, err
}
