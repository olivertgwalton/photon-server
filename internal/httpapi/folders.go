package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// maxFolders is the most subfolders a listing answers; past it, truncated says so.
const maxFolders = 1000

// hiddenFolders is whether a listing shows folders whose names start with a dot.
type hiddenFolders string

const (
	hideHidden hiddenFolders = "hide"
	showHidden hiddenFolders = "show"
)

func hiddenFolderModes() []hiddenFolders { return []hiddenFolders{hideHidden, showHidden} }

type browsedFolderJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// folderListJSON is a folder's subfolders by name, or with no path, the folders to start from.
type folderListJSON struct {
	Path      string              `json:"path,omitzero"`
	Parent    string              `json:"parent,omitzero"`
	Items     []browsedFolderJSON `json:"items"`
	Truncated bool                `json:"truncated,omitzero"`
}

// adminFolders lists the folders on the server, for choosing a library's, as Jellyfin's
// library dialog browses them. Only an admin may: it shows the server's filesystem.
func (a *API) adminFolders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hidden, ok := queryEnum(a, w, r, "hidden", hideHidden, hiddenFolderModes())
	if !ok {
		return
	}
	path := q.Get("path")
	if path == "" {
		writeJSON(w, a.logger, "application/json", http.StatusOK, folderListJSON{Items: folderRoots()})
		return
	}
	if !filepath.IsAbs(path) {
		writeProblem(w, a.logger, codeInvalidParameter, "path is an absolute path")
		return
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, "path is not a folder the server can read")
		return
	}
	out := folderListJSON{Path: path, Items: []browsedFolderJSON{}}
	if parent := filepath.Dir(path); parent != path {
		out.Parent = parent
	}
	for _, e := range entries {
		switch hidden {
		case hideHidden:
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
		case showHidden:
		}
		full := filepath.Join(path, e.Name())
		if !isFolder(full, e) {
			continue
		}
		if len(out.Items) == maxFolders {
			out.Truncated = true
			break
		}
		out.Items = append(out.Items, browsedFolderJSON{Name: e.Name(), Path: full})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// isFolder is whether an entry is a folder, or a link to one, as media is often mounted.
func isFolder(path string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path) //nolint:gosec // browsing the server's folders is the point, and only an admin may
	return err == nil && info.IsDir()
}

// folderRoots are where browsing starts: each drive on Windows; elsewhere the root and, where
// they are there, the folders media is usually mounted under and the server's home.
func folderRoots() []browsedFolderJSON {
	var candidates []string
	if runtime.GOOS == "windows" {
		for d := 'A'; d <= 'Z'; d++ {
			candidates = append(candidates, string(d)+`:\`)
		}
	} else {
		candidates = []string{"/", "/media", "/mnt", "/srv", "/Volumes"}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, home)
		}
	}
	out := []browsedFolderJSON{}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() &&
			!slices.ContainsFunc(out, func(f browsedFolderJSON) bool { return f.Path == c }) {
			out = append(out, browsedFolderJSON{Name: c, Path: c})
		}
	}
	return out
}
