// Package subtitles opens the subtitle files beside copies wherever they are kept.
package subtitles

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// Files are the store, with the subtitle files beside copies found wherever they are kept: in a
// library, or, for one fetched, written from Postgres into dir the first time this node opens it,
// so every node serves it as it serves a file of a library.
type Files struct {
	*store.Store
	dir string
}

func NewFiles(st *store.Store, dir string) (Files, error) {
	return Files{Store: st, dir: dir}, os.MkdirAll(dir, 0o750)
}

// SubtitleFile answers where a subtitle file is, as the store does, a fetched one in dir.
func (f Files) SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error) {
	root, rel, err = f.Store.SubtitleFile(ctx, id)
	if err != nil || root != "" {
		return root, rel, err
	}
	name := path.Base(rel)
	if _, err := os.Stat(filepath.Join(f.dir, name)); !errors.Is(err, fs.ErrNotExist) {
		return f.dir, name, err
	}
	body, err := f.SubtitleBody(ctx, id)
	if err != nil {
		return "", "", err
	}
	tmp, err := os.CreateTemp(f.dir, ".writing-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}
	return f.dir, name, os.Rename(tmp.Name(), filepath.Join(f.dir, name))
}
