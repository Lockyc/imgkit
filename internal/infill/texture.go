package infill

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/lockyc/imgkit/internal/raster"
)

// Texture transfer: the blur fill carries the ground's colour but none of
// its grain, so texture lays the ground's own fine detail over it. The
// detail is each known pixel less a blur of the known pixels at sigma, so it
// holds only what is finer than the fill. Square patches of it, taken from
// around the hole, tile the hole at half-patch steps under a sine window;
// dividing by the root of the summed squared weights keeps the blend of
// independent patches at the ground's own strength rather than averaging it
// down. A patch whose mean or detail energy is an outlier among the
// candidates (it holds an edge, a mark, or a different ground) is never
// used.
const (
	maxPatch   = 128 // patch side, halved until enough patches fit around the hole
	minPatch   = 8
	minSources = 16
	stride     = 4   // candidate patch spacing
	energyCap  = 4.0 // reject a patch with more than this x the median detail energy
	meanCap    = 3.0 // or a mean further than this many robust std-devs from the median
)

// texture reads holes (the image, transparent where the holes are) and fill
// (the blur fill), adds strength x the ground's detail at sigma to fill
// around the holes, and writes the result to out as an opaque PNG.
func texture(holesPath, fillPath, out string, sigma, strength float64, seed int) error {
	holes, err := raster.Load(holesPath)
	if err != nil {
		return err
	}
	fill, err := raster.Load(fillPath)
	if err != nil {
		return err
	}
	b := holes.Bounds()
	if fill.Bounds() != b {
		return fmt.Errorf("the fill is %v and the image %v", fill.Bounds().Size(), b.Size())
	}
	w, h := b.Dx(), b.Dy()
	n := w * h
	// Premultiplied colour and alpha: blurring both and dividing is a blur of
	// the known pixels alone.
	var col [3][]float64
	alpha := make([]float64, n)
	known := make([]bool, n)
	for c := range col {
		col[c] = make([]float64, n)
	}
	hole := image.Rectangle{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := holes.RGBA64At(b.Min.X+x, b.Min.Y+y)
			i := y*w + x
			col[0][i], col[1][i], col[2][i] = float64(p.R)/0xffff, float64(p.G)/0xffff, float64(p.B)/0xffff
			alpha[i] = float64(p.A) / 0xffff
			known[i] = p.A == 0xffff
			if !known[i] {
				hole = hole.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if hole.Empty() {
		return copyOpaque(fill, out)
	}
	wb := blur(alpha, w, h, sigma)
	var det [3][]float64
	for c := range col {
		lo := blur(col[c], w, h, sigma)
		det[c] = make([]float64, n)
		for i := range lo {
			if known[i] && wb[i] > 1e-9 {
				det[c][i] = col[c][i] - lo[i]/wb[i]
			}
		}
	}
	var sources []image.Point
	p := maxPatch
	for ; p >= minPatch; p /= 2 {
		if sources = pickSources(known, col, det, w, h, hole, p); len(sources) >= minSources {
			break
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("no unmasked %dx%d patch near the holes to take the ground's grain from; use --grain 0 for a smooth fill", minPatch, minPatch)
	}
	if p < minPatch {
		p = minPatch
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x696d676b))
	win := make([]float64, p)
	for i := range win {
		win[i] = math.Sin(math.Pi * (float64(i) + 0.5) / float64(p))
	}
	var add [3][]float64
	for c := range add {
		add[c] = make([]float64, n)
	}
	wsq := make([]float64, n)
	for ty := hole.Min.Y - p + p/2; ty < hole.Max.Y; ty += p / 2 {
		for tx := hole.Min.X - p + p/2; tx < hole.Max.X; tx += p / 2 {
			s := sources[rng.IntN(len(sources))]
			for yy := 0; yy < p; yy++ {
				dy := ty + yy
				if dy < 0 || dy >= h {
					continue
				}
				for xx := 0; xx < p; xx++ {
					dx := tx + xx
					if dx < 0 || dx >= w {
						continue
					}
					wt := win[yy] * win[xx]
					si, di := (s.Y+yy)*w+s.X+xx, dy*w+dx
					for c := range add {
						add[c][di] += wt * det[c][si]
					}
					wsq[di] += wt * wt
				}
			}
		}
	}
	img := image.NewRGBA64(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			f := fill.RGBA64At(b.Min.X+x, b.Min.Y+y)
			v := [3]float64{float64(f.R) / 0xffff, float64(f.G) / 0xffff, float64(f.B) / 0xffff}
			if !known[i] && wsq[i] > 0 {
				k := strength / math.Sqrt(wsq[i])
				for c := range v {
					v[c] += k * add[c][i]
				}
			}
			img.SetRGBA64(x, y, color.RGBA64{q16(v[0]), q16(v[1]), q16(v[2]), 0xffff})
		}
	}
	return raster.SavePNG(out, img)
}

// pickSources lists the top-left corners, on a stride grid, of the p x p
// patches that are wholly known and lie within 2p of the holes' bounding
// box, less the outliers by mean and by detail energy.
func pickSources(known []bool, col, det [3][]float64, w, h int, hole image.Rectangle, p int) []image.Point {
	unknown := newSAT(w, h, func(i int) float64 {
		if known[i] {
			return 0
		}
		return 1
	})
	mean := newSAT(w, h, func(i int) float64 { return luma(col, i) })
	energy := newSAT(w, h, func(i int) float64 { l := luma(det, i); return l * l })
	area := hole.Inset(-2 * p).Intersect(image.Rect(0, 0, w, h))
	var pts []image.Point
	var means, energies []float64
	for y := area.Min.Y; y+p <= area.Max.Y; y += stride {
		for x := area.Min.X; x+p <= area.Max.X; x += stride {
			if unknown.sum(x, y, p) > 0 {
				continue
			}
			pts = append(pts, image.Pt(x, y))
			means = append(means, mean.sum(x, y, p))
			energies = append(energies, energy.sum(x, y, p))
		}
	}
	if len(pts) == 0 {
		return nil
	}
	mMed, eMed := median(means), median(energies)
	dev := make([]float64, len(means))
	for i, m := range means {
		dev[i] = math.Abs(m - mMed)
	}
	spread := 1.4826 * median(dev)
	var keep []image.Point
	for i, pt := range pts {
		if energies[i] <= energyCap*eMed && dev[i] <= meanCap*spread {
			keep = append(keep, pt)
		}
	}
	return keep
}

func luma(c [3][]float64, i int) float64 { return 0.2126*c[0][i] + 0.7152*c[1][i] + 0.0722*c[2][i] }

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func q16(v float64) uint16 { return uint16(math.Round(math.Max(0, math.Min(1, v)) * 0xffff)) }

// sat is a summed-area table: the sum over any square in constant time.
type sat struct {
	w int
	v []float64
}

func newSAT(w, h int, f func(i int) float64) sat {
	s := sat{w + 1, make([]float64, (w+1)*(h+1))}
	for y := 0; y < h; y++ {
		var row float64
		for x := 0; x < w; x++ {
			row += f(y*w + x)
			s.v[(y+1)*s.w+x+1] = s.v[y*s.w+x+1] + row
		}
	}
	return s
}

// sum is the total over the p x p square with top-left (x, y).
func (s sat) sum(x, y, p int) float64 {
	return s.v[(y+p)*s.w+x+p] - s.v[y*s.w+x+p] - s.v[(y+p)*s.w+x] + s.v[y*s.w+x]
}

// blur is a Gaussian blur of a w x h plane at sigma, as three box passes
// each way, with edges clamped.
func blur(src []float64, w, h int, sigma float64) []float64 {
	r := max(1, int(math.Round((math.Sqrt(4*sigma*sigma+1)-1)/2)))
	out := slices.Clone(src)
	tmp := make([]float64, len(src))
	for range 3 {
		boxPass(out, tmp, w, h, r, 1, w)
		boxPass(tmp, out, h, w, r, w, 1)
	}
	return out
}

// boxPass box-filters src into dst along lines of length n spaced lines
// apart, with step between neighbours on a line.
func boxPass(src, dst []float64, n, lines, r, step, spacing int) {
	at := func(base, k int) float64 { return src[base+min(max(k, 0), n-1)*step] }
	for l := 0; l < lines; l++ {
		base := l * spacing
		var s float64
		for k := -r; k <= r; k++ {
			s += at(base, k)
		}
		for k := 0; k < n; k++ {
			dst[base+k*step] = s / float64(2*r+1)
			s += at(base, k+r+1) - at(base, k-r)
		}
	}
}

func copyOpaque(fill *image.RGBA64, out string) error {
	img := image.NewRGBA64(fill.Bounds())
	for y := fill.Bounds().Min.Y; y < fill.Bounds().Max.Y; y++ {
		for x := fill.Bounds().Min.X; x < fill.Bounds().Max.X; x++ {
			c := fill.RGBA64At(x, y)
			c.A = 0xffff
			img.SetRGBA64(x, y, c)
		}
	}
	return raster.SavePNG(out, img)
}
