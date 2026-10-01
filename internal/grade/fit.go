package grade

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/lockyc/imgkit/internal/cli"
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
	a := []string{"--subject", rest[0], "--ref", *ref, "--out", rest[1],
		"--level", strconv.Itoa(*level), "--min-inliers", strconv.Itoa(*minInliers)}
	if len(crop) == 1 {
		a = append(a, "--ref-crop", crop[0])
	}
	for _, e := range exclude {
		a = append(a, "--exclude", e)
	}
	res, err := ml.RunScript(ctx, "gradefit.py", a, []string{rest[0], *ref}, []string{rest[1]})
	if err != nil {
		return cli.Fail(stderr, "grade fit", err)
	}
	stdout.Write(res.Stdout)
	return 0
}
