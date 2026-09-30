package doctor

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lockyc/imgkit/internal/engine"
	"github.com/lockyc/imgkit/internal/pins"
)

type getter func(ctx context.Context, url string) (io.ReadCloser, error)

func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// installManaged downloads e's archive, checks its sha256 against the pin,
// unpacks it beside the destination and renames it into place, so an
// interrupted install never leaves a tree that engine.Find accepts.
func installManaged(ctx context.Context, e pins.Engine, get getter) error {
	a, ok := e.Asset()
	if !ok {
		return fmt.Errorf("no %s build for this platform", e.Name)
	}
	dest, err := engine.ManagedDir(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	body, err := get(ctx, a.URL)
	if err != nil {
		return err
	}
	defer body.Close()
	archive := filepath.Join(tmp, "archive.zip")
	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("%s: sha256 %s does not match the pinned %s", a.URL, got, a.SHA256)
	}
	unpacked := filepath.Join(tmp, "unpacked")
	if err := unzip(archive, unpacked); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(unpacked, a.Bin), 0o755); err != nil {
		return fmt.Errorf("archive has no %s: %w", a.Bin, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(unpacked, dest)
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for _, f := range r.File {
		p := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(p, root) {
			return fmt.Errorf("archive entry %q escapes the archive", f.Name)
		}
		mode := f.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if mode&os.ModeSymlink != 0 {
			target, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.Symlink(string(target), p); err != nil {
				return err
			}
			continue
		}
		w, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
		if err == nil {
			_, err = io.Copy(w, rc)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
