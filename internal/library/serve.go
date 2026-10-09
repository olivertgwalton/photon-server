package library

import (
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
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

// Serve sends the first limit bytes of a library file opened from rel, in byte ranges: a .strm's
// media fetched from where it names. Its error, from reading the file's size or fetching the
// media, comes before anything is written.
func Serve(w http.ResponseWriter, r *http.Request, f *os.File, rel string, limit int64) error {
	in, err := Media(rel, f)
	if err != nil {
		return err
	}
	if in.URL != nil {
		return serveRemote(w, r, in.URL, limit)
	}
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

// remoteHeaders are the headers of a .strm's media, as its server answers it, that are passed on:
// what says which bytes they are. A cookie or a redirect is the server's own, not the player's.
var remoteHeaders = []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified"}

// serveRemote passes on the media at u, as Jellyfin streams a .strm's: the player's range asked of
// u's server, which answers whether it has it, as the address itself is not given to the player.
// Of a limit, the first limit bytes are asked for whatever range the player asked.
func serveRemote(w http.ResponseWriter, r *http.Request, u *url.URL, limit int64) error {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), nil)
	if err != nil {
		return err
	}
	limited := limit < math.MaxInt64
	switch {
	case limited:
		req.Header.Set("Range", "bytes=0-"+strconv.FormatInt(limit-1, 10))
	case r.Header.Get("Range") != "":
		req.Header.Set("Range", r.Header.Get("Range"))
		if v := r.Header.Get("If-Range"); v != "" {
			req.Header.Set("If-Range", v)
		}
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // fetching where a .strm names is its point, and only an admin adds a library
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	for _, h := range remoteHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	body := io.Reader(resp.Body)
	// A server that ignores ranges sends all of it, of which only the limit is passed on.
	if limited && resp.StatusCode == http.StatusOK {
		w.Header().Del("Content-Length")
		body = io.LimitReader(body, limit)
	}
	w.WriteHeader(resp.StatusCode)
	// Once the status is sent, a player that stops reading, or a server that stops sending, ends
	// what can be sent, as http.ServeContent's copy ends: there is no answer left to give either.
	io.Copy(w, body) //nolint:errcheck // as said above
	return nil
}
