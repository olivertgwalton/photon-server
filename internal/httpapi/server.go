package httpapi

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Setup is how this node was started, read once from its environment.
type Setup struct {
	Started   time.Time
	Node      uuid.UUID
	Listen    string
	Tools     media.Tools
	Encoder   hls.Hardware
	CacheDir  string
	BackupDir string
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

// serverJSON is the node answering as its dashboard shows it: its role, and transcodes, the videos it
// encodes now, of at most transcode_limit at once, absent when unlimited, as transcode_limit_source
// says. The cluster's nodes are /api/v1/admin/nodes.
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
	YTDLP          toolJSON           `json:"yt_dlp"`
	Encoder        encoderJSON        `json:"encoder"`
	Transcodes     int                `json:"transcodes"`
	Role           domain.NodeRole    `json:"role"`
	TranscodeLimit int                `json:"transcode_limit,omitzero"`
	LimitSource    domain.LimitSource `json:"transcode_limit_source"`
	Listen         string             `json:"listen"`
	Folders        foldersJSON        `json:"folders"`
	// MetadataLanguage and CertificationCountry are what its metadata is asked in, as
	// serverSettingsJSON says.
	MetadataLanguage     string      `json:"metadata_language"`
	CertificationCountry string      `json:"certification_country,omitzero"`
	Postgres             backendJSON `json:"postgres"`
	Valkey               backendJSON `json:"valkey"`
}

// adminServer answers how the node answering was set up and what it reaches, as Jellyfin's
// dashboard shows it; this server is set up by its environment, so all of it is read only.
func (a *API) adminServer(w http.ResponseWriter, r *http.Request) {
	s := a.svc.Setup
	active, _, limit := a.svc.HLS.Transcodes()
	out := serverJSON{
		Info: a.self(), NodeID: s.Node, StartedAt: s.Started.UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		FFmpeg:      toolJSON{s.Tools.FFmpeg.Path, s.Tools.FFmpeg.Version},
		FFprobe:     toolJSON{s.Tools.FFprobe.Path, s.Tools.FFprobe.Version},
		YTDLP:       toolJSON{s.Tools.YTDLP.Path, s.Tools.YTDLP.Version},
		Chromaprint: s.Tools.Chromaprint, Libass: s.Tools.Libass, Encoder: encoderJSON{s.Encoder.Accel, s.Encoder.Device, s.Encoder.HEVC},
		Transcodes: active, TranscodeLimit: limit, Listen: s.Listen,
		Folders:          foldersJSON{folder(s.CacheDir), folder(s.BackupDir)},
		MetadataLanguage: a.svc.Identity.Locale().Language, CertificationCountry: a.svc.Identity.Locale().Country,
	}
	self := a.svc.Placer.Self()
	out.Role, out.LimitSource = self.Role, self.LimitSource
	out.Postgres = a.backend(r, "postgres", a.svc.Postgres)
	out.Valkey = a.backend(r, "valkey", a.svc.Valkey)
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

func (a *API) server(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.self())
}

// self is the server as it says of itself now.
func (a *API) self() domain.Info {
	info := a.info
	info.Name = a.svc.Identity.Name()
	return info
}

type serverSettings interface {
	SetServerSettings(ctx context.Context, set domain.ServerSettings) error
}

// serverSettingsJSON is what the server is called, as clients and the login page show it, and what
// its metadata is asked in where a library or title says none: an IETF language tag, and the
// country certificates are fetched in. An empty name is each node's host's.
type serverSettingsJSON struct {
	Name                 string `json:"name"`
	MetadataLanguage     string `json:"metadata_language"`
	CertificationCountry string `json:"certification_country,omitzero"`
}

// maxServerName is the longest name a server is given, in characters, as a profile's is.
const maxServerName = domain.MaxProfileName

// setServer renames the server and sets what its metadata is asked in, and tells every node.
func (a *API) setServer(w http.ResponseWriter, r *http.Request) {
	var req serverSettingsJSON
	if !a.decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(req.Name) > maxServerName || strings.ContainsFunc(req.Name, unicode.IsControl) {
		writeProblem(w, a.logger, codeInvalidBody, "name is up to "+strconv.Itoa(maxServerName)+" characters, and no control characters")
		return
	}
	lang, refusal := metadataLanguage(req.MetadataLanguage)
	if req.MetadataLanguage == "" {
		refusal = "metadata_language is required: an IETF language tag, such as en-GB"
	}
	country, refusal2 := certificationCountry(req.CertificationCountry)
	if refusal = cmp.Or(refusal, refusal2); refusal != "" {
		writeProblem(w, a.logger, codeInvalidBody, refusal)
		return
	}
	set := domain.ServerSettings{Name: req.Name, Locale: domain.Locale{Language: lang, Country: country}}
	if a.answered(w, r, a.svc.ServerSettings.SetServerSettings(r.Context(), set)) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventServerChanged})
	writeJSON(w, a.logger, "application/json", http.StatusOK, serverSettingsJSON{Name: set.Name, MetadataLanguage: lang, CertificationCountry: country})
}

func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Ready(r.Context()); err != nil {
		// A node draining is not ready by design, asked as often as a balancer likes.
		if !errors.Is(err, domain.ErrStopping) {
			a.logger.WarnContext(r.Context(), "not ready", slog.Any("err", err))
		}
		writeProblem(w, a.logger, codeNotReady, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) serverRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/server", access: public, summary: "Say which server this is",
			status: http.StatusOK, reply: domain.Info{}, handle: a.server,
		},
		{
			pattern: "GET /readyz", access: public, summary: "Say whether Postgres and Valkey are reachable",
			status: http.StatusNoContent, handle: a.readyz,
		},
		{
			pattern: "PUT /api/v1/admin/server", access: admin,
			summary: "Name the server and set what its metadata is asked in, which every node takes up at once",
			body:    serverSettingsJSON{}, status: http.StatusOK, reply: serverSettingsJSON{}, handle: a.setServer,
		},
		{
			pattern: "GET /api/v1/admin/server", access: admin,
			summary: "Say how this node was set up, what it reaches, and the cluster's nodes",
			status:  http.StatusOK, reply: serverJSON{}, handle: a.adminServer,
		},
	}
}
