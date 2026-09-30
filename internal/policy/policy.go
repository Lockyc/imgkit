// Package policy reads imgkit.toml. Its one setting is synthesis: "forbid"
// makes every command that creates pixels the source never had refuse to
// run. The nearest imgkit.toml, from the working directory upward, decides;
// a malformed one is an error, never a silent allow.
package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// File is the policy file's name.
const File = "imgkit.toml"

// ForbiddenError is a synthesising command refused by policy.
type ForbiddenError struct{ Path, Op string }

func (e *ForbiddenError) Error() string {
	return fmt.Sprintf("%s creates pixels the source never had, and %s sets synthesis = \"forbid\"", e.Op, e.Path)
}

type config struct {
	Synthesis string `toml:"synthesis"`
}

// CheckSynthesis applies the nearest imgkit.toml at or above dir to op.
func CheckSynthesis(dir, op string) error {
	path, err := find(dir)
	if err != nil || path == "" {
		return err
	}
	var c config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		keys := make([]string, len(extra))
		for i, k := range extra {
			keys[i] = k.String()
		}
		return fmt.Errorf("%s: unknown setting %s", path, strings.Join(keys, ", "))
	}
	switch c.Synthesis {
	case "", "allow":
		return nil
	case "forbid":
		return &ForbiddenError{Path: path, Op: op}
	default:
		return fmt.Errorf("%s: synthesis = %q; use \"allow\" or \"forbid\"", path, c.Synthesis)
	}
}

func find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, File)
		_, err := os.Stat(p)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
