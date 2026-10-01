package icc

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestSRGB(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p, err := SRGB()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != sha {
		t.Fatalf("materialised profile has the wrong sha256")
	}
}
