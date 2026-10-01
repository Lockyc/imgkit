package quality

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/lockyc/imgkit/internal/raster"
)

// Params are a case's metric parameters, placeholders already expanded.
type Params map[string]any

func (p Params) Path(key string) (string, error) {
	s, ok := p[key].(string)
	if !ok || s == "" {
		return "", fmt.Errorf("param %q: want a file path", key)
	}
	return s, nil
}

func (p Params) String(key, def string) string {
	if s, ok := p[key].(string); ok {
		return s
	}
	return def
}

func (p Params) Float(key string, def float64) (float64, error) {
	switch v := p[key].(type) {
	case nil:
		return def, nil
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	}
	return 0, fmt.Errorf("param %q: want a number", key)
}

func (p Params) Frac(key string) (raster.Frac, bool, error) {
	s, ok := p[key].(string)
	if !ok {
		return raster.Frac{}, false, nil
	}
	f, err := raster.ParseFrac(s)
	return f, true, err
}

// Metric measures a case's output.
type Metric func(Params) (float64, error)

// Metrics is every metric a case may name.
var Metrics = map[string]Metric{
	"rmse": rmse,
}

// rmse compares images a and b. Optional: region (x,y,w,h fractions) and
// channels ("rgb", the default, or "alpha").
func rmse(p Params) (float64, error) {
	ap, err := p.Path("a")
	if err != nil {
		return 0, err
	}
	bp, err := p.Path("b")
	if err != nil {
		return 0, err
	}
	a, err := raster.Load(ap)
	if err != nil {
		return 0, err
	}
	b, err := raster.Load(bp)
	if err != nil {
		return 0, err
	}
	r := a.Bounds()
	if f, ok, err := p.Frac("region"); err != nil {
		return 0, err
	} else if ok {
		r = f.In(r)
	}
	ch := raster.RGB
	switch p.String("channels", "rgb") {
	case "rgb":
	case "alpha":
		ch = raster.Alpha
	default:
		return 0, fmt.Errorf("channels: want rgb or alpha")
	}
	return raster.RMSE(a, b, r, ch)
}

func init() {
	Metrics["dpi"] = dpi
	Metrics["pixel"] = pixel
	Metrics["qr-decodes"] = qrDecodes
}

func dpi(p Params) (float64, error) {
	path, err := p.Path("image")
	if err != nil {
		return 0, err
	}
	return raster.DPI(path)
}

// pixel is the RMS channel distance, 0..1, between the pixel at `at` (x,y
// fractions) and `color`.
func pixel(p Params) (float64, error) {
	path, err := p.Path("image")
	if err != nil {
		return 0, err
	}
	img, err := raster.Load(path)
	if err != nil {
		return 0, err
	}
	xs, ys, _ := strings.Cut(p.String("at", ""), ",")
	fx, err1 := strconv.ParseFloat(xs, 64)
	fy, err2 := strconv.ParseFloat(ys, 64)
	hex := strings.TrimPrefix(p.String("color", ""), "#")
	want, err3 := strconv.ParseUint(hex, 16, 32)
	inUnit := func(f float64) bool { return f >= 0 && f <= 1 } // false for NaN
	if err1 != nil || err2 != nil || err3 != nil || len(hex) != 6 || !inUnit(fx) || !inUnit(fy) {
		return 0, fmt.Errorf("pixel: want at = \"x,y\" and color = \"#rrggbb\"")
	}
	b := img.Bounds()
	c := img.RGBA64At(b.Min.X+int(math.Round(fx*float64(b.Dx()-1))), b.Min.Y+int(math.Round(fy*float64(b.Dy()-1))))
	var sum float64
	for i, v := range []uint16{c.R, c.G, c.B} {
		w := float64((want>>(16-8*i))&0xff) / 255
		d := float64(v)/65535 - w
		sum += d * d
	}
	return math.Sqrt(sum / 3), nil
}

// qrDecodes is 1 when image decodes to text, else 0.
func qrDecodes(p Params) (float64, error) {
	path, err := p.Path("image")
	if err != nil {
		return 0, err
	}
	img, err := raster.Load(path)
	if err != nil {
		return 0, err
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return 0, err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil || res.GetText() != p.String("text", "") {
		return 0, nil
	}
	return 1, nil
}
