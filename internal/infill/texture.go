package infill

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"

	"github.com/lockyc/imgkit/internal/raster"
)

// Texture transfer: the blur fill carries the ground's colour but none of
// its grain, so texture lays the ground's own fine detail over it. The
// detail is each known pixel less a blur of the known pixels at sigma, so it
// holds only what is finer than the fill. Square patches of it, taken from
// around each hole (each connected part of the mask on its own), tile that
// hole at half-patch steps under a sine window; dividing by the root of the
// summed squared weights keeps the blend of independent patches at the
// ground's own strength rather than averaging it down. A patch whose mean
// or detail energy is an outlier among the candidates (it holds an edge, a
// mark, or a different ground) is never used. All of it runs on crops around
// the holes, so its memory follows the holes, not the frame.
const (
	maxPatch   = 128 // patch side, halved until enough patches fit around the hole
	minPatch   = 8
	minSources = 16
	stride     = 4   // candidate patch spacing
	energyCap  = 4.0 // reject a patch with more than this x the median detail energy
	meanCap    = 3.0 // or a mean further than this many robust std-devs from the median
)

// piece is a grained crop of the fill, transparent outside the holes, to be
// composited over the fill at its offset.
type piece struct {
	path string
	at   image.Point
}

// component is one connected hole: its label in the label map and its
// bounding box.
type component struct {
	label int32
	box   image.Rectangle
}

// texture reads holes (the image, transparent where the holes are) and fill
// (the blur fill), and writes into dir one piece per cluster of nearby holes:
// the fill there plus strength x the ground's detail at sigma.
func texture(holesPath, fillPath, dir string, sigma, strength float64, seed int) ([]piece, error) {
	holes, err := decode(holesPath)
	if err != nil {
		return nil, err
	}
	fill, err := decode(fillPath)
	if err != nil {
		return nil, err
	}
	frame := holes.Bounds()
	if fill.Bounds() != frame {
		return nil, fmt.Errorf("the fill is %v and the image %v", fill.Bounds().Size(), frame.Size())
	}
	labels, comps := label(holes)
	if len(comps) == 0 {
		return nil, nil
	}
	// A hole's crop reaches its farthest source patch, plus the blur's
	// footprint around that.
	reach := 2*maxPatch + int(math.Ceil(3*sigma))
	rng := rand.New(rand.NewPCG(uint64(seed), 0x696d676b))
	var pieces []piece
	for i, cl := range cluster(comps, reach, frame) {
		c := newCrop(holes, labels, cl.region, sigma)
		out := image.NewRGBA64(cl.holes)
		for _, comp := range cl.members {
			if err := c.grain(comp, sigma, strength, fill, out, rng); err != nil {
				return nil, err
			}
		}
		p := piece{filepath.Join(dir, fmt.Sprintf("grain%d.png", i)), cl.holes.Min}
		if err := raster.SavePNG(p.path, out); err != nil {
			return nil, err
		}
		pieces = append(pieces, p)
	}
	return pieces, nil
}

func decode(path string) (image.RGBA64Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if r, ok := img.(image.RGBA64Image); ok {
		return r, nil
	}
	return nil, fmt.Errorf("%s: unsupported pixel format", path)
}

// label numbers the 8-connected parts of the holes (pixels not fully
// opaque) from 1; known pixels are 0. The map is frame-relative.
func label(holes image.RGBA64Image) ([]int32, []component) {
	b := holes.Bounds()
	w, h := b.Dx(), b.Dy()
	labels := make([]int32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if holes.RGBA64At(b.Min.X+x, b.Min.Y+y).A != 0xffff {
				labels[y*w+x] = -1
			}
		}
	}
	var comps []component
	var stack []int
	for start, l := range labels {
		if l != -1 {
			continue
		}
		c := component{label: int32(len(comps) + 1)}
		labels[start] = c.label
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%w, i/w
			c.box = c.box.Union(image.Rect(x, y, x+1, y+1))
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx >= 0 && nx < w && ny >= 0 && ny < h && labels[ny*w+nx] == -1 {
						labels[ny*w+nx] = c.label
						stack = append(stack, ny*w+nx)
					}
				}
			}
		}
		comps = append(comps, c)
	}
	return labels, comps
}

// clusterOf is holes whose crops overlap, so they share one crop.
type clusterOf struct {
	members []component
	region  image.Rectangle // the crop, frame-relative
	holes   image.Rectangle // the members' bounding box, frame-relative
}

func cluster(comps []component, reach int, frame image.Rectangle) []clusterOf {
	full := image.Rect(0, 0, frame.Dx(), frame.Dy())
	var cls []clusterOf
	for _, c := range comps {
		cls = append(cls, clusterOf{[]component{c}, c.box.Inset(-reach).Intersect(full), c.box})
	}
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(cls) && !merged; i++ {
			for j := i + 1; j < len(cls); j++ {
				if cls[i].region.Overlaps(cls[j].region) {
					cls[i].members = append(cls[i].members, cls[j].members...)
					cls[i].region = cls[i].region.Union(cls[j].region)
					cls[i].holes = cls[i].holes.Union(cls[j].holes)
					cls = slices.Delete(cls, j, j+1)
					merged = true
					break
				}
			}
		}
	}
	return cls
}

// crop holds one cluster's region: the detail, the label of each pixel, and
// summed-area tables over it for choosing patches.
type crop struct {
	r                     image.Rectangle // frame-relative
	w, h                  int
	labels                []int32
	det                   [3][]float32
	unknown, mean, energy sat
}

func newCrop(holes image.RGBA64Image, labels []int32, r image.Rectangle, sigma float64) *crop {
	fb := holes.Bounds()
	c := &crop{r: r, w: r.Dx(), h: r.Dy()}
	n := c.w * c.h
	c.labels = make([]int32, n)
	alpha := make([]float32, n)
	var col [3][]float32
	for k := range col {
		col[k] = make([]float32, n)
	}
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			i := y*c.w + x
			c.labels[i] = labels[(r.Min.Y+y)*fb.Dx()+r.Min.X+x]
			p := holes.RGBA64At(fb.Min.X+r.Min.X+x, fb.Min.Y+r.Min.Y+y)
			// Premultiplied colour and alpha: blurring both and dividing is
			// a blur of the known pixels alone.
			col[0][i], col[1][i], col[2][i] = float32(p.R)/0xffff, float32(p.G)/0xffff, float32(p.B)/0xffff
			alpha[i] = float32(p.A) / 0xffff
		}
	}
	c.unknown = newSAT(c.w, c.h, func(i int) float64 {
		if c.labels[i] != 0 {
			return 1
		}
		return 0
	})
	c.mean = newSAT(c.w, c.h, func(i int) float64 { return luma(col, i) })
	c.detail(col, alpha, sigma)
	return c
}

// detail turns col into the ground's detail at sigma, in place.
func (c *crop) detail(col [3][]float32, alpha []float32, sigma float64) {
	wb := blur(alpha, c.w, c.h, sigma)
	for k := range col {
		lo := blur(col[k], c.w, c.h, sigma)
		for i := range lo {
			if c.labels[i] == 0 && wb[i] > 1e-6 {
				col[k][i] -= lo[i] / wb[i]
			} else {
				col[k][i] = 0
			}
		}
	}
	c.det = col
	c.energy = newSAT(c.w, c.h, func(i int) float64 { l := luma(col, i); return l * l })
}

// grain adds strength x detail to comp's pixels of fill, into out.
func (c *crop) grain(comp component, sigma, strength float64, fill image.RGBA64Image, out *image.RGBA64, rng *rand.Rand) error {
	box := comp.box.Sub(c.r.Min) // crop-relative
	var sources []image.Point
	p := maxPatch
	for {
		sources = c.sources(box, p)
		if len(sources) >= minSources || p == minPatch {
			break
		}
		p /= 2
	}
	if len(sources) == 0 {
		return fmt.Errorf("no unmasked %dx%d patch near the hole at %v to take the ground's grain from; use --grain 0 for a smooth fill", minPatch, minPatch, comp.box.Min)
	}
	win := make([]float64, p)
	for i := range win {
		win[i] = math.Sin(math.Pi * (float64(i) + 0.5) / float64(p))
	}
	bw, bh := box.Dx(), box.Dy()
	var add [3][]float64
	for k := range add {
		add[k] = make([]float64, bw*bh)
	}
	wsq := make([]float64, bw*bh)
	for ty := box.Min.Y - p/2; ty < box.Max.Y; ty += p / 2 {
		for tx := box.Min.X - p/2; tx < box.Max.X; tx += p / 2 {
			s := sources[rng.IntN(len(sources))]
			for yy := 0; yy < p; yy++ {
				dy := ty + yy
				if dy < box.Min.Y || dy >= box.Max.Y {
					continue
				}
				for xx := 0; xx < p; xx++ {
					dx := tx + xx
					if dx < box.Min.X || dx >= box.Max.X || c.labels[dy*c.w+dx] != comp.label {
						continue
					}
					wt := win[yy] * win[xx]
					si, di := (s.Y+yy)*c.w+s.X+xx, (dy-box.Min.Y)*bw+dx-box.Min.X
					for k := range add {
						add[k][di] += wt * float64(c.det[k][si])
					}
					wsq[di] += wt * wt
				}
			}
		}
	}
	fb := fill.Bounds()
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			i := y*bw + x
			if wsq[i] == 0 {
				continue
			}
			fx, fy := c.r.Min.X+box.Min.X+x, c.r.Min.Y+box.Min.Y+y // frame-relative
			f := fill.RGBA64At(fb.Min.X+fx, fb.Min.Y+fy)
			k := strength / math.Sqrt(wsq[i])
			out.SetRGBA64(fx, fy, color.RGBA64{
				q16(float64(f.R)/0xffff + k*add[0][i]),
				q16(float64(f.G)/0xffff + k*add[1][i]),
				q16(float64(f.B)/0xffff + k*add[2][i]),
				0xffff,
			})
		}
	}
	return nil
}

// sources lists the top-left corners, on a stride grid, of the p x p patches
// that are wholly known and lie within 2p of box, less the outliers by mean
// and by detail energy.
func (c *crop) sources(box image.Rectangle, p int) []image.Point {
	area := box.Inset(-2 * p).Intersect(image.Rect(0, 0, c.w, c.h))
	var pts []image.Point
	var means, energies []float64
	for y := area.Min.Y; y+p <= area.Max.Y; y += stride {
		for x := area.Min.X; x+p <= area.Max.X; x += stride {
			if c.unknown.sum(x, y, p) > 0 {
				continue
			}
			pts = append(pts, image.Pt(x, y))
			means = append(means, c.mean.sum(x, y, p))
			energies = append(energies, c.energy.sum(x, y, p))
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

func luma(c [3][]float32, i int) float64 {
	return 0.2126*float64(c[0][i]) + 0.7152*float64(c[1][i]) + 0.0722*float64(c[2][i])
}

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
func blur(src []float32, w, h int, sigma float64) []float32 {
	r := boxRadius(sigma)
	out := slices.Clone(src)
	tmp := make([]float32, len(src))
	for range 3 {
		boxPass(out, tmp, w, h, r, 1, w)
		boxPass(tmp, out, h, w, r, w, 1)
	}
	return out
}

// boxPass box-filters src into dst along lines of length n spaced lines
// apart, with step between neighbours on a line.
func boxPass(src, dst []float32, n, lines, r, step, spacing int) {
	at := func(base, k int) float64 { return float64(src[base+min(max(k, 0), n-1)*step]) }
	for l := 0; l < lines; l++ {
		base := l * spacing
		var s float64
		for k := -r; k <= r; k++ {
			s += at(base, k)
		}
		for k := 0; k < n; k++ {
			dst[base+k*step] = float32(s / float64(2*r+1))
			s += at(base, k+r+1) - at(base, k-r)
		}
	}
}

// boxRadius is the radius of each of blur's three box passes at sigma, so
// its support is three times this.
func boxRadius(sigma float64) int {
	return max(1, int(math.Round((math.Sqrt(4*sigma*sigma+1)-1)/2)))
}
