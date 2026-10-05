package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// streamFor is how long a stream's address stays good: longer than anyone watches in a sitting.
const streamFor = 24 * time.Hour

type playing interface {
	Playable(ctx context.Context, item, version uuid.UUID) (uuid.UUID, []store.PlayPart, error)
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
}

type partJSON struct {
	ID         uuid.UUID `json:"id"`
	URL        string    `json:"url"`
	OffsetMS   int64     `json:"offset_ms"`
	DurationMS int64     `json:"duration_ms"`
}

// play answers where a film or episode plays from: its copy's files in order, each with where it
// starts on the copy's timeline, at signed addresses a player fetches directly.
func (a *API) play(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VersionID string `json:"version_id"`
	}
	// A body is only for asking for one copy rather than the longest.
	if r.ContentLength != 0 && !a.decode(w, r, &req) {
		return
	}
	var version uuid.UUID
	if req.VersionID != "" {
		var err error
		if version, err = uuid.Parse(req.VersionID); err != nil {
			writeProblem(w, a.logger, codeInvalidBody, "version_id is not an id")
			return
		}
	}
	id, ok := a.titleID(w, r)
	if !ok {
		return
	}
	version, parts, err := a.svc.Playing.Playable(r.Context(), id, version)
	if a.answered(w, r, err) {
		return
	}
	until := time.Now().Add(streamFor)
	out := make([]partJSON, len(parts))
	for i, p := range parts {
		out[i] = partJSON{
			ID: p.ID, URL: a.svc.Signer.Sign("/api/v1/parts/"+p.ID.String()+"/stream", until),
			OffsetMS: p.OffsetMS, DurationMS: p.DurationMS,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, struct {
		VersionID uuid.UUID  `json:"version_id"`
		Parts     []partJSON `json:"parts"`
		ExpiresAt time.Time  `json:"expires_at"`
	}{version, out, until.UTC().Truncate(time.Second)})
}

// videoTypes are the types of the containers a library holds, which Go's own table lacks.
var videoTypes = map[string]string{
	".mkv": "video/x-matroska", ".mk3d": "video/x-matroska", ".webm": "video/webm", ".mp4": "video/mp4",
	".m4v": "video/x-m4v", ".mov": "video/quicktime", ".ts": "video/mp2t", ".m2ts": "video/mp2t",
	".mts": "video/mp2t", ".avi": "video/x-msvideo", ".wmv": "video/x-ms-wmv", ".mpg": "video/mpeg",
	".mpeg": "video/mpeg", ".ogv": "video/ogg", ".flv": "video/x-flv",
}

// partStream serves one file of a copy as it is, in byte ranges.
func (a *API) partStream(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	root, rel, err := a.svc.Playing.PartFile(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	lib, err := os.OpenRoot(root)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer lib.Close()
	f, err := lib.Open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if t, ok := videoTypes[strings.ToLower(path.Ext(rel))]; ok {
		w.Header().Set("Content-Type", t)
	}
	http.ServeContent(w, r, rel, info.ModTime(), f)
}

// requireSignature admits a request whose address the server signed and which has not lapsed.
func (a *API) requireSignature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !a.svc.Signer.Valid(r.URL.Path, q.Get("exp"), q.Get("sig"), time.Now()) {
			writeProblem(w, a.logger, codeUnauthenticated, "the address is not signed, or has lapsed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
