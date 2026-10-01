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
// median gradient in a ring around the hole. Each connected part the flood
// misses is cut off from the blur levels only if it is another ground: at
// least as large as the smallest hole it could feed (a smaller part cannot
// outweigh the ring in a blur that spans both), with some of it below the
// limit, and that part's median tone more than stopK robust std-devs
// from the ring's. Specks, fibres and the pockets they enclose are the
// hole's own ground and stay. A hole that straddles an edge touches both
// grounds, so it keeps both.
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
// the gradient, and reach how far the coarsest level reaches from a hole.
func edgeStops(holesPath, dir string, smooth float64, reach int) ([]piece, error) {
	holes, err := decode(holesPath)
	if err != nil {
		return nil, err
	}
	labels, comps := label(holes)
	var pieces []piece
	for i, cl := range cluster(comps, reach, holes.Bounds()) {
		smallest := cl.members[0].area
		for _, m := range cl.members {
			smallest = min(smallest, m.area)
		}
		drop := stopped(holes, labels, cl.region, smooth, smallest)
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

// stopped floods region r from its holes' borders and returns the parts it
// missed that are another ground of at least minPart pixels, black on
// transparent, or nil if there are none.
func stopped(holes image.RGBA64Image, labels []int32, r image.Rectangle, smooth float64, minPart int) *image.NRGBA {
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
	// A missed part holding no ground below the limit is an edge line or a
	// speck; one smaller than minPart, or of the ring's tone, is a pocket of
	// the same ground. An edge line goes with the ground it joins.
	seen := make([]bool, n)
	var drop *image.NRGBA
	for start := range known {
		if !known[start] || reached[start] || seen[start] {
			continue
		}
		seen[start] = true
		part := []int{start}
		flood([]int{start}, w, h, func(i int) bool { return known[i] && !reached[i] && !seen[i] }, func(i int) {
			seen[i] = true
			part = append(part, i)
		})
		var flat []float64
		for _, i := range part {
			if grad[i] <= limit {
				flat = append(flat, float64(lo[i]))
			}
		}
		if len(flat) < minPart || math.Abs(median(flat)-ground) <= spread {
			continue
		}
		if drop == nil {
			drop = image.NewNRGBA(image.Rect(0, 0, w, h))
		}
		for _, i := range part {
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
