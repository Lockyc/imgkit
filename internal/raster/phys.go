package raster

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
)

var pngSig = []byte("\x89PNG\r\n\x1a\n")

// SetDPI records dpi in a PNG's pHYs chunk, replacing any already there,
// without touching the pixel data. A render of tens of megapixels is not
// worth a full decode and re-encode for nine bytes.
func SetDPI(path string, dpi float64) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	chunks, err := split(b)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	ppm := uint32(math.Round(dpi / 0.0254))
	data := make([]byte, 9)
	binary.BigEndian.PutUint32(data[0:], ppm)
	binary.BigEndian.PutUint32(data[4:], ppm)
	data[8] = 1 // unit: metre
	var out bytes.Buffer
	out.Write(pngSig)
	for _, c := range chunks {
		if c.typ == "pHYs" {
			continue
		}
		out.Write(c.raw)
		if c.typ == "IHDR" {
			writeChunk(&out, "pHYs", data)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".phys-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DPI reads a PNG's pHYs resolution; 0 when it has none or none in metres.
func DPI(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	chunks, err := split(b)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	for _, c := range chunks {
		if c.typ == "pHYs" && len(c.data) == 9 && c.data[8] == 1 {
			return float64(binary.BigEndian.Uint32(c.data[0:])) * 0.0254, nil
		}
	}
	return 0, nil
}

type chunk struct {
	typ       string
	data, raw []byte
}

func split(b []byte) ([]chunk, error) {
	if !bytes.HasPrefix(b, pngSig) {
		return nil, fmt.Errorf("not a PNG")
	}
	var out []chunk
	for pos := len(pngSig); pos < len(b); {
		if pos+8 > len(b) {
			return nil, fmt.Errorf("truncated chunk header")
		}
		n := int(binary.BigEndian.Uint32(b[pos:]))
		end := pos + 12 + n
		if n < 0 || end > len(b) {
			return nil, fmt.Errorf("truncated chunk")
		}
		out = append(out, chunk{typ: string(b[pos+4 : pos+8]), data: b[pos+8 : pos+8+n], raw: b[pos:end]})
		pos = end
	}
	return out, nil
}

func writeChunk(w *bytes.Buffer, typ string, data []byte) {
	binary.Write(w, binary.BigEndian, uint32(len(data)))
	body := append([]byte(typ), data...)
	w.Write(body)
	binary.Write(w, binary.BigEndian, crc32.ChecksumIEEE(body))
}
