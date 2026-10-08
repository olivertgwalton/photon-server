package library

import (
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// fileTypes are the types of the files a library holds, which Go's own table lacks.
var fileTypes = map[string]string{
	".mkv": "video/x-matroska", ".mk3d": "video/x-matroska", ".webm": "video/webm", ".mp4": "video/mp4",
	".m4v": "video/x-m4v", ".mov": "video/quicktime", ".ts": "video/mp2t", ".m2ts": "video/mp2t",
	".mts": "video/mp2t", ".avi": "video/x-msvideo", ".wmv": "video/x-ms-wmv", ".mpg": "video/mpeg",
	".mpeg": "video/mpeg", ".ogv": "video/ogg", ".flv": "video/x-flv",
	".srt": "application/x-subrip", ".vtt": "text/vtt", ".ass": "text/x-ssa", ".ssa": "text/x-ssa",
}

// Serve sends the first limit bytes of a library file opened from rel, in byte ranges. Its error,
// from reading the file's size, comes before anything is written.
func Serve(w http.ResponseWriter, r *http.Request, f *os.File, rel string, limit int64) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if t, ok := fileTypes[strings.ToLower(path.Ext(rel))]; ok {
		w.Header().Set("Content-Type", t)
	}
	if limit >= info.Size() {
		// The bare file keeps the copy in the kernel: sendfile takes only an *os.File.
		http.ServeContent(w, r, rel, info.ModTime(), f)
		return nil
	}
	http.ServeContent(w, r, rel, info.ModTime(), io.NewSectionReader(f, 0, limit))
	return nil
}
