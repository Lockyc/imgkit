package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, File)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoFileAllows(t *testing.T) {
	if err := CheckSynthesis(t.TempDir(), "inpaint"); err != nil {
		t.Fatal(err)
	}
}

func TestForbidInParent(t *testing.T) {
	root := t.TempDir()
	p := write(t, root, `synthesis = "forbid"`)
	child := filepath.Join(root, "a", "b")
	os.MkdirAll(child, 0o755)
	err := CheckSynthesis(child, "upscale")
	var fe *ForbiddenError
	if !errors.As(err, &fe) || fe.Path != p || fe.Op != "upscale" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error %q does not name %s", err, p)
	}
}

func TestNearestWins(t *testing.T) {
	root := t.TempDir()
	write(t, root, `synthesis = "forbid"`)
	child := filepath.Join(root, "sub")
	os.MkdirAll(child, 0o755)
	write(t, child, `synthesis = "allow"`)
	if err := CheckSynthesis(child, "infill"); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedIsAnError(t *testing.T) {
	for _, body := range []string{`synthesis = "forbidden"`, `synthesys = "forbid"`, `synthesis = `} {
		dir := t.TempDir()
		p := write(t, dir, body)
		err := CheckSynthesis(dir, "inpaint")
		if err == nil {
			t.Errorf("%q accepted", body)
			continue
		}
		var fe *ForbiddenError
		if errors.As(err, &fe) || !strings.Contains(err.Error(), p) {
			t.Errorf("%q: err = %v; want a parse error naming the file", body, err)
		}
	}
}
