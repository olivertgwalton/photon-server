package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Setup is how this node was started, read once from its environment.
type Setup struct {
	Started          time.Time
	Node             uuid.UUID
	Listen           string
	Tools            media.Tools
	Encoder          hls.Hardware
	Discovery        domain.Discovery
	MetadataLanguage string
	CacheDir         string
	BackupDir        string
}

type versioned interface {
	Version(ctx context.Context) (string, error)
}

type cluster interface {
	versioned
	Nodes(ctx context.Context) ([]kv.Node, error)
}

type toolJSON struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type encoderJSON struct {
	Acceleration domain.Acceleration `json:"acceleration"`
	Device       string              `json:"device,omitzero"`
}

// folderJSON's free_bytes is absent where the server cannot tell.
type folderJSON struct {
	Path      string  `json:"path"`
	FreeBytes *uint64 `json:"free_bytes,omitzero"`
}

type foldersJSON struct {
	Cache   folderJSON `json:"cache"`
	Backups folderJSON `json:"backups"`
}

type backendJSON struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitzero"`
}

type nodeJSON struct {
	ID       uuid.UUID `json:"id"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
}

// serverJSON is the server as its dashboard shows it. transcode_limit is absent when unlimited.
type serverJSON struct {
	Info
	NodeID           uuid.UUID        `json:"node_id"`
	StartedAt        time.Time        `json:"started_at"`
	OS               string           `json:"os"`
	Arch             string           `json:"arch"`
	FFmpeg           toolJSON         `json:"ffmpeg"`
	FFprobe          toolJSON         `json:"ffprobe"`
	Chromaprint      bool             `json:"chromaprint"`
	Encoder          encoderJSON      `json:"encoder"`
	TranscodeLimit   int              `json:"transcode_limit,omitzero"`
	Discovery        domain.Discovery `json:"discovery"`
	Listen           string           `json:"listen"`
	TrustedProxies   []string         `json:"trusted_proxies"`
	Folders          foldersJSON      `json:"folders"`
	MetadataLanguage string           `json:"metadata_language"`
	Postgres         backendJSON      `json:"postgres"`
	Valkey           backendJSON      `json:"valkey"`
	Nodes            []nodeJSON       `json:"nodes"`
}

// adminServer answers how the node answering was set up and what it reaches, as Jellyfin's
// dashboard shows it; this server is set up by its environment, so all of it is read only.
func (a *API) adminServer(w http.ResponseWriter, r *http.Request) {
	s := a.svc.Setup
	_, _, limit := a.svc.HLS.Transcodes()
	out := serverJSON{
		Info: a.info, NodeID: s.Node, StartedAt: s.Started.UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		FFmpeg:      toolJSON{s.Tools.FFmpeg.Path, s.Tools.FFmpeg.Version},
		FFprobe:     toolJSON{s.Tools.FFprobe.Path, s.Tools.FFprobe.Version},
		Chromaprint: s.Tools.Chromaprint, Encoder: encoderJSON{s.Encoder.Accel, s.Encoder.Device},
		TranscodeLimit: limit, Discovery: s.Discovery, Listen: s.Listen, TrustedProxies: []string{},
		Folders:          foldersJSON{folder(s.CacheDir), folder(s.BackupDir)},
		MetadataLanguage: s.MetadataLanguage, Nodes: []nodeJSON{},
	}
	for _, p := range a.svc.TrustedProxies {
		out.TrustedProxies = append(out.TrustedProxies, p.String())
	}
	out.Postgres = a.backend(r, "postgres", a.svc.Postgres)
	if out.Valkey = a.backend(r, "valkey", a.svc.Valkey); out.Valkey.Reachable {
		nodes, err := a.svc.Valkey.Nodes(r.Context())
		if err != nil {
			a.internal(w, r, err)
			return
		}
		for _, n := range nodes {
			out.Nodes = append(out.Nodes, nodeJSON{n.ID, n.Address, n.Seen.UTC()})
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// backend says whether a backend answers, and its version. Why it does not is logged, not
// answered: the error may name where it is.
func (a *API) backend(r *http.Request, name string, b versioned) backendJSON {
	v, err := b.Version(r.Context())
	if err != nil {
		a.logger.WarnContext(r.Context(), "unreachable", slog.String("backend", name), slog.Any("err", err))
		return backendJSON{}
	}
	return backendJSON{Reachable: true, Version: v}
}

func folder(path string) folderJSON {
	f := folderJSON{Path: path}
	if free, ok := freeBytes(path); ok {
		f.FreeBytes = &free
	}
	return f
}
