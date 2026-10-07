package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Setup is how this node was started, read once from its environment.
type Setup struct {
	Started time.Time
	Node    uuid.UUID
	Listen  string
	Tools   media.Tools
	Encoder hls.Hardware
	// LimitSource is where the limit on transcodes at once comes from.
	LimitSource      domain.LimitSource
	Discovery        domain.Discovery
	MetadataLanguage string
	CacheDir         string
	BackupDir        string
	// PublicURL is where readers reach the web app, where it is set.
	PublicURL *url.URL
}

type versioned interface {
	Version(ctx context.Context) (string, error)
}

type cluster interface {
	versioned
	Nodes(ctx context.Context) ([]domain.Node, error)
}

type toolJSON struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type encoderJSON struct {
	Acceleration domain.Acceleration `json:"acceleration"`
	Device       string              `json:"device,omitzero"`
	// HEVC is whether video is encoded to HEVC: deny where it is set so, or the device would not.
	HEVC domain.HEVCEncoding `json:"hevc"`
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

// nodeJSON is a node as it tells the others of itself: what it encodes with, and transcodes, of
// which conversions are downloads', of at most transcode_limit at once, absent when unlimited.
type nodeJSON struct {
	ID          uuid.UUID          `json:"id"`
	Name        string             `json:"name,omitzero"`
	Address     string             `json:"address"`
	LastSeen    time.Time          `json:"last_seen"`
	Encoder     nodeEncoderJSON    `json:"encoder"`
	Transcodes  int                `json:"transcodes"`
	Conversions int                `json:"conversions"`
	Limit       int                `json:"transcode_limit,omitzero"`
	LimitSource domain.LimitSource `json:"transcode_limit_source"`
}

// nodeEncoderJSON is what a node encodes video with, and whether it draws styled subtitles in.
type nodeEncoderJSON struct {
	Acceleration domain.Acceleration `json:"acceleration"`
	HEVC         domain.HEVCEncoding `json:"hevc"`
	Libass       bool                `json:"libass"`
}

func showNode(n domain.Node) nodeJSON {
	return nodeJSON{
		ID: n.ID, Name: n.Name, Address: n.Address, LastSeen: n.Seen.UTC(),
		Encoder:    nodeEncoderJSON{n.Encoder.Acceleration, n.Encoder.HEVC, n.Encoder.Libass},
		Transcodes: n.Transcodes, Conversions: n.Conversions, Limit: n.Limit, LimitSource: n.LimitSource,
	}
}

// serverJSON is the server as its dashboard shows it: transcodes, the videos this node encodes now,
// of at most transcode_limit at once, absent when unlimited, set as transcode_limit_source says.
type serverJSON struct {
	domain.Info
	NodeID      uuid.UUID `json:"node_id"`
	StartedAt   time.Time `json:"started_at"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	FFmpeg      toolJSON  `json:"ffmpeg"`
	FFprobe     toolJSON  `json:"ffprobe"`
	Chromaprint bool      `json:"chromaprint"`
	Libass      bool      `json:"libass"`
	// YTDLP fetches theme tunes from ThemerrDB's links; its path and version are empty without it.
	YTDLP            toolJSON           `json:"yt_dlp"`
	Encoder          encoderJSON        `json:"encoder"`
	Transcodes       int                `json:"transcodes"`
	TranscodeLimit   int                `json:"transcode_limit,omitzero"`
	LimitSource      domain.LimitSource `json:"transcode_limit_source"`
	Discovery        domain.Discovery   `json:"discovery"`
	Listen           string             `json:"listen"`
	PublicURL        string             `json:"public_url,omitzero"`
	TrustedProxies   []string           `json:"trusted_proxies"`
	Folders          foldersJSON        `json:"folders"`
	MetadataLanguage string             `json:"metadata_language"`
	Postgres         backendJSON        `json:"postgres"`
	Valkey           backendJSON        `json:"valkey"`
	Nodes            []nodeJSON         `json:"nodes"`
}

// adminServer answers how the node answering was set up and what it reaches, as Jellyfin's
// dashboard shows it; this server is set up by its environment, so all of it is read only.
func (a *API) adminServer(w http.ResponseWriter, r *http.Request) {
	s := a.svc.Setup
	active, _, limit := a.svc.HLS.Transcodes()
	out := serverJSON{
		Info: a.info, NodeID: s.Node, StartedAt: s.Started.UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		FFmpeg:      toolJSON{s.Tools.FFmpeg.Path, s.Tools.FFmpeg.Version},
		FFprobe:     toolJSON{s.Tools.FFprobe.Path, s.Tools.FFprobe.Version},
		YTDLP:       toolJSON{s.Tools.YTDLP.Path, s.Tools.YTDLP.Version},
		Chromaprint: s.Tools.Chromaprint, Libass: s.Tools.Libass, Encoder: encoderJSON{s.Encoder.Accel, s.Encoder.Device, s.Encoder.HEVC},
		Transcodes: active, TranscodeLimit: limit, LimitSource: s.LimitSource, Discovery: s.Discovery, Listen: s.Listen, TrustedProxies: []string{},
		Folders:          foldersJSON{folder(s.CacheDir), folder(s.BackupDir)},
		MetadataLanguage: s.MetadataLanguage, Nodes: []nodeJSON{},
	}
	if s.PublicURL != nil {
		out.PublicURL = s.PublicURL.String()
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
			out.Nodes = append(out.Nodes, showNode(n))
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
