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
// wall, a frame around a print) bleeds into it. Only the ground a hole can
// reach without crossing a strong edge feeds its fill: a flood from the
// pixels bordering the hole through known pixels whose gradient, on a
// light blur of the known pixels, is at most stopK robust std-devs above
// the median gradient in a ring around the hole. A hole that straddles an
// edge touches both grounds, so it keeps both.
const (
	stopK = 4.0 // gradient threshold, in robust std-devs above the ring's median
	// A gradient below one 8-bit level per pixel is never an edge, however
	// flat the ground (a flat ground has no spread to set a threshold by).
	minStop = 1.0 / 255
)

// edgeStops reads holes and returns pieces, black where a known pixel lies
// beyond a strong edge from every hole it could feed, transparent
// elsewhere; nil when there are none. smooth is the blur before the
// gradient, and reach how far the coarsest level reaches from a hole.
func edgeStops(holesPath, dir string, smooth float64, reach int) ([]piece, error) {
	holes, err := decode(holesPath)
	if err != nil {
		return nil, err
	}
	labels, comps := label(holes)
	var pieces []piece
	for i, cl := range cluster(comps, reach, holes.Bounds()) {
		drop := stopped(holes, labels, cl.region, smooth)
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

// stopped floods region r from its holes' borders and returns the known
// pixels it did not reach, black on transparent, or nil if it reached all.
func stopped(holes image.RGBA64Image, labels []int32, r image.Rectangle, smooth float64) *image.NRGBA {
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
	var ring []float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !known[y*w+x] {
				continue
			}
			x0, y0, x1, y1 := max(x-d, 0), max(y-d, 0), min(x+d+1, w), min(y+d+1, h)
			if unknown.v[y1*unknown.w+x1]-unknown.v[y0*unknown.w+x1]-unknown.v[y1*unknown.w+x0]+unknown.v[y0*unknown.w+x0] > 0 {
				ring = append(ring, grad[y*w+x])
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
	reached := make([]bool, n)
	var stack []int
	visit := func(i int) {
		if known[i] && !reached[i] && grad[i] <= limit {
			reached[i] = true
			stack = append(stack, i)
		}
	}
	for i := range known {
		if known[i] {
			continue
		}
		x, y := i%w, i/w
		if x > 0 {
			visit(i - 1)
		}
		if x < w-1 {
			visit(i + 1)
		}
		if y > 0 {
			visit(i - w)
		}
		if y < h-1 {
			visit(i + w)
		}
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%w, i/w
		if x > 0 {
			visit(i - 1)
		}
		if x < w-1 {
			visit(i + 1)
		}
		if y > 0 {
			visit(i - w)
		}
		if y < h-1 {
			visit(i + w)
		}
	}
	var drop *image.NRGBA
	for i := range known {
		if known[i] && !reached[i] {
			if drop == nil {
				drop = image.NewNRGBA(image.Rect(0, 0, w, h))
			}
			drop.SetNRGBA(i%w, i/w, color.NRGBA{0, 0, 0, 255})
		}
	}
	return drop
}
