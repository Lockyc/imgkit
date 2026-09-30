package quality

import (
	"fmt"

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
