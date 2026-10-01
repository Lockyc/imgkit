// Package engine is the one way imgkit runs an external tool. Every call gets
// a timeout; success means exit status 0 (or one listed in OKExit) and every
// declared output existing and non-empty, because Chrome exits 0 when it
// renders nothing and prints noise on stderr when it succeeds; a declared
// output is deleted before the run and again if the run fails, so a failure
// never leaves a plausible-looking file behind; and stderr is fatal where a
// call says so (Ghostscript, whose stderr is the only sign it dropped an
// image).
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lockyc/imgkit/internal/pins"
)

// DefaultTimeout applies when a Cmd sets none.
const DefaultTimeout = 10 * time.Minute

// EnvOverride names the variable that points imgkit at a specific executable
// for an engine, ahead of the data directory and PATH.
func EnvOverride(name string) string {
	return "IMGKIT_ENGINE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// DataDir holds managed engines: $XDG_DATA_HOME/imgkit, else ~/.local/share/imgkit.
func DataDir() (string, error) { return xdg("XDG_DATA_HOME", ".local/share") }

// CacheDir holds rebuildable state (materialised scripts, compiled helpers):
// $XDG_CACHE_HOME/imgkit, else ~/.cache/imgkit.
func CacheDir() (string, error) { return xdg("XDG_CACHE_HOME", ".cache") }

func xdg(env, fallback string) (string, error) {
	if d := os.Getenv(env); d != "" {
		return filepath.Join(d, "imgkit"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback, "imgkit"), nil
}

// ManagedDir is where a managed engine's pinned version is unpacked.
func ManagedDir(e pins.Engine) (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, e.Name, e.Download.Version), nil
}

// NotInstalledError says an engine is missing and how to get it.
type NotInstalledError struct{ Engine, Hint string }

func (e *NotInstalledError) Error() string {
	return e.Engine + " is not installed: " + e.Hint
}

// Resolve finds the executable for the named engine.
func Resolve(name string) (string, error) {
	e, ok := pins.Lookup(name)
	if !ok {
		return "", fmt.Errorf("engine %q is not in the pin table", name)
	}
	return Find(e)
}

// Find locates e: its override variable, then (Managed) the data directory
// or (Minimum) PATH.
func Find(e pins.Engine) (string, error) {
	if p := os.Getenv(EnvOverride(e.Name)); p != "" {
		return p, nil
	}
	missing := &NotInstalledError{Engine: e.Name, Hint: e.Hint()}
	if !e.Supported() {
		return "", missing
	}
	if e.Kind == pins.Managed {
		a, ok := e.Asset()
		if !ok {
			return "", missing
		}
		dir, err := ManagedDir(e)
		if err != nil {
			return "", err
		}
		p := filepath.Join(dir, a.Bin)
		if fi, err := os.Stat(p); err == nil && fi.Mode()&0o111 != 0 {
			return p, nil
		}
		return "", missing
	}
	p, err := exec.LookPath(e.Name)
	if err != nil {
		return "", missing
	}
	return p, nil
}

// Cmd is one engine invocation. Inputs and Outputs are paths as imgkit sees
// them, relative to its own working directory rather than Dir.
type Cmd struct {
	Engine      string // pin-table name; resolves the executable unless Path is set
	Path        string // an executable imgkit built itself; Engine is then only a label
	Args        []string
	Dir         string
	Env         []string // added to the inherited environment
	Stdin       io.Reader
	Timeout     time.Duration // 0 means DefaultTimeout
	StderrFatal bool          // any stderr output fails the call
	OKExit      []int         // non-zero exit statuses that are not failures
	Inputs      []string      // files the engine reads; none may also be an output
	Outputs     []string      // files that must exist and be non-empty afterwards
}

// Result is what the engine printed.
type Result struct{ Stdout, Stderr []byte }

// RunError is a failed engine call.
type RunError struct{ Engine, Reason, Stderr string }

func (e *RunError) Error() string {
	msg := e.Engine + ": " + e.Reason
	if s := lastLines(e.Stderr, 20); s != "" {
		msg += "\n" + s
	}
	return msg
}

// Run executes c under the package rules. It refuses a call whose output is
// also one of its inputs before touching anything, because deleting the
// stale output would destroy the source.
func Run(ctx context.Context, c Cmd) (Result, error) {
	for _, o := range c.Outputs {
		for _, in := range c.Inputs {
			if SameFile(in, o) {
				return Result{}, fmt.Errorf("%s: %s is both an input and an output; write the result elsewhere", c.Engine, o)
			}
		}
	}
	for _, o := range c.Outputs {
		if err := os.Remove(o); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Result{}, err
		}
	}
	path := c.Path
	if path == "" {
		p, err := Resolve(c.Engine)
		if err != nil {
			return Result{}, err
		}
		path = p
	}
	res, err := run(ctx, path, c)
	if err != nil {
		for _, o := range c.Outputs {
			os.Remove(o)
		}
	}
	return res, err
}

// SameFile reports whether a and b name one file: the same inode when both
// exist, else the same absolute path.
func SameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(fa, fb)
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// run gives the engine files, not pipes, for stdin, stdout and stderr, so
// Wait returns when the engine exits rather than when the last process
// holding its output closes it; then it kills the engine's process group,
// so nothing the engine left behind outlives the call.
func run(ctx context.Context, path string, c Cmd) (Result, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	stdout, err := scratch()
	if err != nil {
		return Result{}, err
	}
	defer stdout.Close()
	stderr, err := scratch()
	if err != nil {
		return Result{}, err
	}
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if c.Stdin != nil {
		stdin, err := scratch()
		if err != nil {
			return Result{}, err
		}
		defer stdin.Close()
		if _, err := io.Copy(stdin, c.Stdin); err != nil {
			return Result{}, err
		}
		if _, err := stdin.Seek(0, io.SeekStart); err != nil {
			return Result{}, err
		}
		cmd.Stdin = stdin
	}
	killGroupOnCancel(cmd)
	if err = cmd.Start(); err == nil {
		err = cmd.Wait()
		killGroup(cmd)
	}
	res := Result{Stdout: readAll(stdout), Stderr: readAll(stderr)}
	fail := func(reason string) (Result, error) {
		return res, &RunError{Engine: c.Engine, Reason: reason, Stderr: string(res.Stderr)}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fail("timed out after " + timeout.String())
	case ctx.Err() != nil:
		return fail("cancelled")
	}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return fail(err.Error())
		}
		if !slices.Contains(c.OKExit, ee.ExitCode()) {
			return fail(ee.Error())
		}
	}
	if c.StderrFatal && len(bytes.TrimSpace(res.Stderr)) > 0 {
		return fail("reported a problem on stderr")
	}
	for _, o := range c.Outputs {
		if fi, err := os.Stat(o); err != nil || fi.Size() == 0 {
			return fail("produced no output at " + o)
		}
	}
	return res, nil
}

// scratch is an anonymous temporary file: unlinked at once, gone on Close.
func scratch() (*os.File, error) {
	f, err := os.CreateTemp("", "imgkit-")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// readAll reads a scratch file the engine wrote from the start.
func readAll(f *os.File) []byte {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	b, _ := io.ReadAll(f)
	return b
}

// Version reads a Minimum engine's version. The exit status is ignored,
// because some tools exit non-zero from their version flag, and stdout and
// stderr are read together, because some print it on stderr.
func Version(ctx context.Context, e pins.Engine) (string, error) {
	if e.Kind != pins.Minimum {
		return "", fmt.Errorf("%s: version is pinned by its install path, not read", e.Name)
	}
	path, err := Find(e)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, path, e.VersionArgs...).CombinedOutput()
	m := regexp.MustCompile(e.VersionRe).FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("%s: no version in %q", e.Name, lastLines(string(out), 1))
	}
	return string(m[1]), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
