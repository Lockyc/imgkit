package icc

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/lockyc/imgkit/internal/engine"
)

func TestSRGB(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p, err := SRGB()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != sha {
		t.Fatalf("materialised profile has the wrong sha256")
	}
}

// TestSRGBIgnoresAStaleTemp: a temp file another run left (or is writing)
// at a fixed name does not stop the write.
func TestSRGBIgnoresAStaleTemp(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cache, err := engine.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cache, "icc", sha[:16]+"-sRGB2014.icc")
	if err := os.MkdirAll(p+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SRGB(); err != nil {
		t.Fatal(err)
	}
}
