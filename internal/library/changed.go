package library

import (
	"os"
	"path"
	"path/filepath"

	"github.com/olivertgwalton/photon-server/internal/naming"
)

// Changed is the folder of the library at root to read again for a change at p: p itself if it is
// a folder, else the nearest folder above it that is there, as Jellyfin's monitor refreshes the
// nearest ancestor that exists. A Subs or theme-music folder is read as part of its parent. ok is
// false for a path outside the library.
func Changed(root, p string) (dir string, ok bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", false
	}
	for dir = filepath.ToSlash(rel); dir != "."; dir = path.Dir(dir) {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir)))
		if _, folded := naming.FoldedFolder(path.Base(dir)); err == nil && info.IsDir() && !folded {
			return dir, true
		}
	}
	return ".", true
}
