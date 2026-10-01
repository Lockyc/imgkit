package ml

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lockyc/imgkit/internal/enginetest"
	"github.com/lockyc/imgkit/internal/pins"
)

func TestEveryScriptIsLockedAndDated(t *testing.T) {
	names, _ := fs.Glob(scripts, "scripts/*.py")
	for _, n := range names {
		b, _ := scripts.ReadFile(n)
		if !strings.Contains(string(b), `exclude-newer = "`) {
			t.Errorf("%s: PEP 723 header lacks [tool.uv] exclude-newer", n)
		}
		if _, err := scripts.ReadFile(n + ".lock"); err != nil {
			t.Errorf("%s: no committed lockfile; run `just lock-ml`", n)
		}
	}
}

func TestLocksAreCurrent(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	names, _ := filepath.Glob("scripts/*.py")
	for _, n := range names {
		if out, err := exec.Command("uv", "lock", "--check", "--script", n).CombinedOutput(); err != nil {
			t.Errorf("%s: lockfile is stale; run `just lock-ml`\n%s", n, out)
		}
	}
}

func TestScriptCacheKeyedByContent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	names, _ := fs.Glob(scripts, "scripts/*.py")
	if len(names) == 0 {
		t.Skip("no scripts yet")
	}
	name := filepath.Base(names[0])
	p1, err := Script(name)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p1, []byte("tampered"), 0o644)
	os.RemoveAll(filepath.Dir(p1))
	p2, _ := Script(name)
	if p1 != p2 {
		t.Errorf("same content, different cache path: %s vs %s", p1, p2)
	}
	b, _ := os.ReadFile(p2)
	want, _ := scripts.ReadFile("scripts/" + name)
	if string(b) != string(want) {
		t.Error("materialised script differs from the embedded one")
	}
	if _, err := os.Stat(p2 + ".lock"); err != nil {
		t.Error("lockfile not materialised beside the script")
	}
}

func TestRunScriptAndTool(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	log := enginetest.Stub(t, "uv", `exit 0`)
	names, _ := fs.Glob(scripts, "scripts/*.py")
	if len(names) > 0 {
		name := filepath.Base(names[0])
		if _, err := RunScript(context.Background(), name, []string{"--x", "1"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		c := enginetest.Calls(t, log)[0]
		if !slices.Equal(c[:3], []string{"run", "--locked", "--script"}) || !strings.HasSuffix(c[3], name) || !slices.Equal(c[4:], []string{"--x", "1"}) {
			t.Errorf("RunScript args %q", c)
		}
	}
	if _, err := RunTool(context.Background(), pins.PyTool{Package: "iopaint", Version: "1.6.0", ExcludeNewer: "2026-10-01T00:00:00Z"}, "iopaint", []string{"run"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	calls := enginetest.Calls(t, log)
	want := []string{"tool", "run", "--from", "iopaint==1.6.0", "--exclude-newer", "2026-10-01T00:00:00Z", "iopaint", "run"}
	if !slices.Equal(calls[len(calls)-1], want) {
		t.Errorf("RunTool args %q", calls[len(calls)-1])
	}
}
