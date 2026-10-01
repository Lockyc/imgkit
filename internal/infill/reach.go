package infill

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"

	"github.com/lockyc/imgkit/internal/raster"
)

// Edge stops: the blur levels average everything within their reach, so a
// different ground across a strong edge near a hole (a skirting below a
// wall, a frame around a print) bleeds into it. A flood from the pixels
// bordering the hole runs through known pixels whose gradient, on a light
// blur of the known pixels, is at most stopK robust std-devs above the
// median gradient in a ring around the hole. Each connected part of the
// ground the flood misses (pixels at or below the limit) is cut off from the
// blur levels only if it is another ground: its median tone is more than
// stopK robust std-devs from the ring's, and that difference times its pull
// on the holes (the most the coarsest level draws from it at any hole
// pixel, as a share of all it draws there) is at least one 8-bit level, so
// it would visibly shift the fill. The pull falls with distance from the
// holes, not with how far the crop around them runs. The edge pixels joined
// to a cut-off part go with it. So a rail with the hole's own ground beyond
// it is cut off and that ground is kept; specks, fibres and the pockets they
// enclose stay. A hole that straddles an edge touches both grounds, so it
// keeps both.
const (
	stopK = 4.0 // gradient threshold, in robust std-devs above the ring's median
	// Less than one 8-bit level (of gradient per pixel, or of tone) never
	// marks an edge or another ground, however flat the ground: a flat
	// ground has no spread to set a threshold by.
	minStop = 1.0 / 255
)

// edgeStops reads holes and returns pieces, black over the known pixels of
// another ground cut off by a strong edge from the holes they could feed,
// transparent elsewhere; nil when there are none. smooth is the blur before
// the gradient, and coarse the coarsest level's sigma: it weighs each
// ground's pull, and its 3 sigma reach sets the crop around the holes.
func edgeStops(holesPath, dir string, smooth, coarse float64) ([]piece, error) {
	reach := int(math.Ceil(3 * coarse))
	holes, err := decode(holesPath)
	if err != nil {
		return nil, err
	}
	labels, comps := label(holes)
	var pieces []piece
	for i, cl := range cluster(comps, reach, holes.Bounds()) {
		drop := stopped(holes, labels, cl.region, smooth, coarse)
		if drop == nil {
			continue
		}
		p := piece{filepath.Join(dir, fmt.Sprintf("stop%d.png", i)), cl.region.Min}
		if err := raster.SavePNG(p.path, drop); err != nil {
			return nil, err
		}
		pieces = append(pieces, p)
	}
	return pieces, nil
}

// stopped floods region r from its holes' borders and returns the missed
// parts that are another ground, with the edge pixels joined to them, black
// on transparent, or nil if there are none.
func stopped(holes image.RGBA64Image, labels []int32, r image.Rectangle, smooth, coarse float64) *image.NRGBA {
	fb := holes.Bounds()
	w, h := r.Dx(), r.Dy()
	n := w * h
	known := make([]bool, n)
	lum, wt := make([]float32, n), make([]float32, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			known[i] = labels[(r.Min.Y+y)*fb.Dx()+r.Min.X+x] == 0
			if known[i] {
				p := holes.RGBA64At(fb.Min.X+r.Min.X+x, fb.Min.Y+r.Min.Y+y)
				lum[i] = float32((0.2126*float64(p.R) + 0.7152*float64(p.G) + 0.0722*float64(p.B)) / 0xffff)
				wt[i] = 1
			}
		}
	}
	lo, wb := blur(lum, w, h, smooth), blur(wt, w, h, smooth)
	for i := range lo {
		if wb[i] > 1e-6 {
			lo[i] /= wb[i]
		}
	}
	grad := make([]float64, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			gx := float64(lo[y*w+min(x+1, w-1)] - lo[y*w+max(x-1, 0)])
			gy := float64(lo[min(y+1, h-1)*w+x] - lo[max(y-1, 0)*w+x])
			grad[y*w+x] = math.Hypot(gx, gy) / 2
		}
	}
	// The ring: known pixels within 4 x smooth of a hole.
	d := max(1, int(math.Ceil(4*smooth)))
	unknown := newSAT(w, h, func(i int) float64 {
		if known[i] {
			return 0
		}
		return 1
	})
	var ring, tone []float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !known[y*w+x] {
				continue
			}
			x0, y0, x1, y1 := max(x-d, 0), max(y-d, 0), min(x+d+1, w), min(y+d+1, h)
			if unknown.v[y1*unknown.w+x1]-unknown.v[y0*unknown.w+x1]-unknown.v[y1*unknown.w+x0]+unknown.v[y0*unknown.w+x0] > 0 {
				ring = append(ring, grad[y*w+x])
				tone = append(tone, float64(lo[y*w+x]))
			}
		}
	}
	if len(ring) == 0 {
		return nil
	}
	med := median(ring)
	dev := make([]float64, len(ring))
	for i, g := range ring {
		dev[i] = math.Abs(g - med)
	}
	limit := max(med+stopK*1.4826*median(dev), minStop)
	ground := median(tone)
	for i, v := range tone {
		dev[i] = math.Abs(v - ground)
	}
	spread := max(stopK*1.4826*median(dev), minStop)
	// Flood from the holes' borders through pixels at or below the limit.
	reached := make([]bool, n)
	var seeds []int
	for i := range known {
		if !known[i] {
			seeds = append(seeds, i)
		}
	}
	flood(seeds, w, h, func(i int) bool { return known[i] && !reached[i] && grad[i] <= limit }, func(i int) { reached[i] = true })
	// The flood misses ground pixels (at or below the limit) and edge pixels
	// (above it). Each connected part of the missed ground is judged on its
	// own: it is another ground if its tone is off the ring's by more than
	// the spread, and by enough, weighted by what the coarsest level draws
	// from it into the holes, to shift the fill a level; otherwise it is a
	// pocket of the same ground. The edge pixels joined to a cut-off part
	// then go with it; edge pixels joined to none (specks, fibres, an edge
	// line between two kept grounds) stay.
	missedGround := func(i int) bool { return known[i] && !reached[i] && grad[i] <= limit }
	var pl *puller // made once a part needs it
	seen := make([]bool, n)
	cut := make([]bool, n)
	var cutOff []int
	for start := range known {
		if !missedGround(start) || seen[start] {
			continue
		}
		seen[start] = true
		part := []int{start}
		box := image.Rect(start%w, start/w, start%w+1, start/w+1)
		flood([]int{start}, w, h, func(i int) bool { return missedGround(i) && !seen[i] }, func(i int) {
			seen[i] = true
			part = append(part, i)
			box = box.Union(image.Rect(i%w, i/w, i%w+1, i/w+1))
		})
		tones := make([]float64, len(part))
		for k, i := range part {
			tones[k] = float64(lo[i])
		}
		off := math.Abs(median(tones) - ground)
		if off <= spread {
			continue
		}
		if pl == nil {
			pl = newPuller(known, wt, w, h, coarse)
		}
		if !pl.pullsAtLeast(part, box, minStop/off) {
			continue
		}
		for _, i := range part {
			cut[i] = true
		}
		cutOff = append(cutOff, part...)
	}
	if len(cutOff) == 0 {
		return nil
	}
	flood(cutOff, w, h, func(i int) bool { return known[i] && !reached[i] && !cut[i] && grad[i] > limit }, func(i int) { cut[i] = true })
	drop := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, c := range cut {
		if c {
			drop.SetNRGBA(i%w, i/w, color.NRGBA{0, 0, 0, 255})
		}
	}
	return drop
}

// flood visits, 4-connected from seeds, every pixel pass admits, calling
// take on each before passing on from it. Seeds themselves are not taken.
func flood(seeds []int, w, h int, pass func(i int) bool, take func(i int)) {
	stack := append([]int(nil), seeds...)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%w, i/w
		for _, j := range [4]int{i - 1, i + 1, i - w, i + w} {
			if (j == i-1 && x == 0) || (j == i+1 && x == w-1) || (j == i-w && y == 0) || (j == i+w && y == h-1) {
				continue
			}
			if pass(j) {
				take(j)
				stack = append(stack, j)
			}
		}
	}
}

// puller weighs a missed part by its pull on one crop's holes: the most the
// coarsest blur level, at sigma coarse, draws from the part at any hole
// pixel, over what it draws there from all the known pixels. The pull
// depends only on the part and the ground near the holes, not on how far
// the crop runs.
type puller struct {
	w, h   int
	coarse float64
	known  []bool
	knownW []float32       // the known pixels, blurred
	holes  image.Rectangle // the hole pixels' bounding box
	reach  int             // the blur's support: no pixel draws on one further off
	// The most a pixel in each column (row) gives, along that axis, to any
	// hole column (row), and the least knownW at a hole pixel: a part's
	// pull is at most the sum of its pixels' column x row weights over it.
	col, row []float64
	floor    float64
}

func newPuller(known []bool, wt []float32, w, h int, coarse float64) *puller {
	p := &puller{w: w, h: h, coarse: coarse, known: known, knownW: blur(wt, w, h, coarse), floor: math.Inf(1)}
	for i, k := range known {
		if !k {
			p.holes = p.holes.Union(image.Rect(i%w, i/w, i%w+1, i/w+1))
			if p.knownW[i] > 1e-6 {
				p.floor = min(p.floor, float64(p.knownW[i]))
			}
		}
	}
	p.reach = 3 * boxRadius(coarse)
	p.col = p.most(w, p.holes.Min.X, p.holes.Max.X)
	p.row = p.most(h, p.holes.Min.Y, p.holes.Max.Y)
	return p
}

// most is, for each position on a line of n, the most the blur moves from
// it to any position in [lo, hi). Away from the line's ends that is the
// kernel at the gap; within a support of an end the blur's clamping weighs
// positions more, so there it is measured.
func (p *puller) most(n, lo, hi int) []float64 {
	r := boxRadius(p.coarse)
	impulse := func(s int) []float32 {
		// blur's passes along one line of n.
		d, tmp := make([]float32, n), make([]float32, n)
		d[s] = 1
		for range 3 {
			boxPass(d, tmp, n, 1, r, 1, n)
			d, tmp = tmp, d
		}
		return d
	}
	var kernel []float32
	if c := n / 2; c >= p.reach && c < n-p.reach {
		kernel = impulse(c)[c : c+p.reach+1]
	}
	out := make([]float64, n)
	for s := range n {
		if s >= p.reach && s < n-p.reach {
			if g := max(0, lo-s, s-(hi-1)); g <= p.reach {
				out[s] = float64(kernel[g])
			}
			continue
		}
		for _, v := range impulse(s)[lo:hi] {
			out[s] = max(out[s], float64(v))
		}
	}
	return out
}

// pullsAtLeast reports whether part, whose bounding box is box, pulls on
// the holes at least least. A part whose bound falls short is settled from
// its pixels' positions alone; any other part is blurred over only the
// window where it meets the holes.
func (p *puller) pullsAtLeast(part []int, box image.Rectangle, least float64) bool {
	var bound float64
	for _, i := range part {
		bound += p.col[i%p.w] * p.row[i/p.w]
	}
	// 1.001: headroom for float32 rounding in the blurs this bounds.
	if bound/p.floor*1.001 < least {
		return false
	}
	// Past the window's edges the part's blur is zero, or the holes lie a
	// full support inside them, so the window blurs exactly as the crop would.
	win := box.Inset(-p.reach).Intersect(p.holes.Inset(-p.reach)).Intersect(image.Rect(0, 0, p.w, p.h))
	ww, wh := win.Dx(), win.Dy()
	ind := make([]float32, ww*wh)
	for _, i := range part {
		ind[(i/p.w-win.Min.Y)*ww+i%p.w-win.Min.X] = 1
	}
	pw := blur(ind, ww, wh, p.coarse)
	at := win.Intersect(p.holes)
	for y := at.Min.Y; y < at.Max.Y; y++ {
		for x := at.Min.X; x < at.Max.X; x++ {
			i := y*p.w + x
			if !p.known[i] && p.knownW[i] > 1e-6 && float64(pw[(y-win.Min.Y)*ww+x-win.Min.X]/p.knownW[i]) >= least {
				return true
			}
		}
	}
	return false
}
