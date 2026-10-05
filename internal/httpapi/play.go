package httpapi

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// streamFor is how long a stream's address stays good: longer than anyone watches in a sitting.
const streamFor = 24 * time.Hour

type playbacks interface {
	Start(ctx context.Context, profile, item, version uuid.UUID, method domain.PlayMethod) (domain.Playback, error)
	Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState) (domain.Reach, error)
	Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error)
}

type remuxing interface {
	Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan) error
}

type hlsFiles interface {
	Playlist(playback uuid.UUID, name string) (string, error)
	SubtitleSegment(ctx context.Context, playback uuid.UUID, track, n int) (string, error)
	Init(ctx context.Context, playback uuid.UUID, part int) (*os.File, error)
	Segment(ctx context.Context, playback uuid.UUID, n int) (*os.File, error)
}

type playing interface {
	Playable(ctx context.Context, item, version uuid.UUID) (store.PlayCopy, error)
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error)
}

type partJSON struct {
	ID         uuid.UUID `json:"id"`
	URL        string    `json:"url"`
	OffsetMS   int64     `json:"offset_ms"`
	DurationMS int64     `json:"duration_ms"`
}

// subtitleJSON is a subtitle file beside a copy played as it is, timed on the copy's timeline.
type subtitleJSON struct {
	ID              uuid.UUID `json:"id"`
	Codec           string    `json:"codec"`
	Language        string    `json:"language,omitzero"`
	Title           string    `json:"title,omitzero"`
	Default         bool      `json:"default,omitzero"`
	Forced          bool      `json:"forced,omitzero"`
	HearingImpaired bool      `json:"hearing_impaired,omitzero"`
	URL             string    `json:"url"`
}

type videoJSON struct {
	Stream      int                        `json:"stream"`
	Decision    decision                   `json:"decision"`
	DolbyVision domain.DolbyVisionHandling `json:"dolby_vision,omitzero"`
	Codec       string                     `json:"codec,omitzero"`
	Width       int                        `json:"width,omitzero"`
	Height      int                        `json:"height,omitzero"`
	BitrateKbps int                        `json:"bitrate_kbps,omitzero"`
	ToneMapped  bool                       `json:"tone_mapped,omitzero"`
	// BurnedSubtitle is the subtitle stream drawn into the picture.
	BurnedSubtitle *int `json:"burned_subtitle,omitzero"`
}

type audioJSON struct {
	Stream      int      `json:"stream"`
	Decision    decision `json:"decision"`
	Codec       string   `json:"codec,omitzero"`
	Channels    int      `json:"channels,omitzero"`
	BitrateKbps int      `json:"bitrate_kbps,omitzero"`
}

// decision is what becomes of a stream, in Plex's words.
type decision string

const (
	decisionCopy      decision = "copy"
	decisionTranscode decision = "transcode"
)

// play opens a playback of a film or episode as the client's profile decides: its copy's files in
// order, each with where it starts on the copy's timeline, or an HLS playlist of them, its video
// copied or encoded, at signed
// addresses a player fetches directly. It says what becomes of each stream, and why the copy could
// not be played as it is.
func (a *API) play(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VersionID   string `json:"version_id"`
		AudioStream *int   `json:"audio_stream"`
		// SubtitleStream is a subtitle the client will show; one that is a picture is drawn into the
		// video where the client cannot draw it.
		SubtitleStream *int              `json:"subtitle_stream"`
		Profile        *playback.Profile `json:"profile"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	if req.Profile == nil {
		writeProblem(w, a.logger, codeInvalidBody, "profile says what the client plays")
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
	c, err := a.svc.Playing.Playable(r.Context(), id, version)
	if a.answered(w, r, err) {
		return
	}
	d, err := playback.Decide(*req.Profile, playback.Copy{Container: c.Container, BitrateKbps: c.BitrateKbps, Streams: c.Streams}, req.AudioStream, req.SubtitleStream)
	switch {
	case errors.Is(err, playback.ErrNoSuchAudio):
		writeProblem(w, a.logger, codeInvalidBody, "audio_stream is not one of the copy's audio streams")
		return
	case errors.Is(err, playback.ErrNoSuchSubtitle):
		writeProblem(w, a.logger, codeInvalidBody, "subtitle_stream is not one of the copy's subtitle streams")
		return
	case errors.Is(err, playback.ErrNoCompatibleStream):
		status := codeNoCompatibleStream.status()
		writeJSON(w, a.logger, "application/problem+json", status, struct {
			problem
			Reasons []playback.Reason `json:"reasons"`
		}{problem{Title: http.StatusText(status), Status: status, Code: codeNoCompatibleStream}, d.Reasons})
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	session, err := a.svc.Playbacks.Start(r.Context(), sessionOf(r).Profile.ID, id, c.Version, d.Method)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	until := time.Now().Add(streamFor)
	answer := struct {
		PlaybackID uuid.UUID         `json:"playback_id"`
		Method     domain.PlayMethod `json:"method"`
		VersionID  uuid.UUID         `json:"version_id"`
		Video      *videoJSON        `json:"video,omitzero"`
		Audio      *audioJSON        `json:"audio,omitzero"`
		Reasons    []playback.Reason `json:"reasons,omitzero"`
		Parts      []partJSON        `json:"parts,omitzero"`
		Subtitles  []subtitleJSON    `json:"subtitles,omitzero"`
		Playlist   string            `json:"playlist,omitzero"`
		ExpiresAt  time.Time         `json:"expires_at"`
	}{
		PlaybackID: session.ID, Method: d.Method, VersionID: c.Version, Reasons: d.Reasons,
		ExpiresAt: until.UTC().Truncate(time.Second),
	}
	if v := d.Video; v != nil {
		answer.Video = &videoJSON{Stream: v.Stream, Decision: decisionCopy, DolbyVision: v.DolbyVision}
		if e := v.Encode; e != nil {
			answer.Video = &videoJSON{
				Stream: v.Stream, Decision: decisionTranscode, Codec: e.Codec, Width: e.Width, Height: e.Height,
				BitrateKbps: e.BitrateKbps, ToneMapped: e.ToneMap, BurnedSubtitle: e.Burn,
			}
		}
	}
	if au := d.Audio; au != nil {
		answer.Audio = &audioJSON{Stream: au.Stream, Decision: decisionCopy}
		if e := au.Encode; e != nil {
			answer.Audio.Decision, answer.Audio.Codec = decisionTranscode, e.Codec
			answer.Audio.Channels, answer.Audio.BitrateKbps = e.Channels, e.BitrateKbps
		}
	}
	if d.Method != domain.PlayDirect {
		if err := a.svc.Remuxing.Open(r.Context(), session.ID, c, *d.Video, d.Audio); err != nil {
			a.internal(w, r, err)
			return
		}
		subject := hlsSubject(session.ID)
		exp, sig := a.svc.Signer.Token(subject, until)
		answer.Playlist = subject + "/" + exp + "/" + sig + "/main.m3u8"
	} else {
		for _, p := range c.Parts {
			answer.Parts = append(answer.Parts, partJSON{
				ID: p.ID, URL: a.svc.Signer.Sign("/api/v1/parts/"+p.ID.String()+"/stream", until),
				OffsetMS: p.OffsetMS, DurationMS: p.DurationMS,
			})
		}
		for _, f := range c.Subtitles {
			sub := subtitleJSON{
				ID: f.ID, Codec: f.Codec, Title: f.Title, Default: f.Default, Forced: f.Forced,
				HearingImpaired: f.HearingImpaired, URL: a.svc.Signer.Sign("/api/v1/subtitles/"+f.ID.String()+"/file", until),
			}
			if f.Language != language.Und {
				sub.Language = f.Language.String()
			}
			answer.Subtitles = append(answer.Subtitles, sub)
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, answer)
}

func hlsSubject(playback uuid.UUID) string { return "/api/v1/hls/" + playback.String() }

// hlsFile serves a remux's playlist, a part's initialisation or a segment, made as they are asked
// for. The playlist addresses everything else relative to itself, so one signature covers it all.
func (a *API) hlsFile(w http.ResponseWriter, r *http.Request) {
	playback, err := uuid.Parse(r.PathValue("playback"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	name := r.PathValue("file")
	var f *os.File
	switch {
	case strings.HasSuffix(name, ".m3u8"):
		playlist, err := a.svc.HLS.Playlist(playback, name)
		if a.answeredRemux(w, r, err) {
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, playlist)
		return
	case strings.HasPrefix(name, "sub") && strings.HasSuffix(name, ".vtt"):
		track, n, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(name, "sub"), ".vtt"), "-")
		t, terr := strconv.Atoi(track)
		k, kerr := strconv.Atoi(n)
		if !ok || terr != nil || kerr != nil {
			writeProblem(w, a.logger, codeNotFound, "")
			return
		}
		vtt, err := a.svc.HLS.SubtitleSegment(r.Context(), playback, t, k)
		if a.answeredRemux(w, r, err) {
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		_, _ = io.WriteString(w, vtt)
		return
	case strings.HasPrefix(name, "init") && strings.HasSuffix(name, ".mp4"):
		part, perr := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "init"), ".mp4"))
		if perr != nil {
			writeProblem(w, a.logger, codeNotFound, "")
			return
		}
		f, err = a.svc.HLS.Init(r.Context(), playback, part)
		w.Header().Set("Content-Type", "video/mp4")
	case strings.HasSuffix(name, ".m4s"):
		n, perr := strconv.Atoi(strings.TrimSuffix(name, ".m4s"))
		if perr != nil {
			writeProblem(w, a.logger, codeNotFound, "")
			return
		}
		f, err = a.svc.HLS.Segment(r.Context(), playback, n)
		w.Header().Set("Content-Type", "video/iso.segment")
	default:
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if a.answeredRemux(w, r, err) {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (a *API) answeredRemux(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, hls.ErrNoRemux):
		writeProblem(w, a.logger, codeNotFound, "the playback has stopped, or lapsed")
	case errors.Is(err, context.Canceled):
	case err != nil:
		a.internal(w, r, err)
	default:
		return false
	}
	return true
}

// requireSignedPath admits a request whose path carries a signature of its HLS playback, as
// /api/v1/hls/{playback}/{exp}/{sig}/…, that has not lapsed.
func (a *API) requireSignedPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject := "/api/v1/hls/" + r.PathValue("playback")
		if !a.svc.Signer.Valid(subject, r.PathValue("exp"), r.PathValue("sig"), time.Now()) {
			writeProblem(w, a.logger, codeUnauthenticated, "the address is not signed, or has lapsed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// playbackProgress is the player saying where it has got to, every ten seconds or so.
func (a *API) playbackProgress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PositionMS int64  `json:"position_ms"`
		State      string `json:"state"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	state, ok := domain.ParsePlayState(req.State)
	if !ok || req.PositionMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "position_ms is not negative and state is playing or paused")
		return
	}
	a.reportPlayback(w, r, func(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error) {
		return a.svc.Playbacks.Progress(ctx, profile, id, time.Duration(req.PositionMS)*time.Millisecond, state)
	})
}

// playbackStop is the player saying it has stopped, and where.
func (a *API) playbackStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PositionMS int64 `json:"position_ms"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	if req.PositionMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "position_ms is not negative")
		return
	}
	a.reportPlayback(w, r, func(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error) {
		return a.svc.Playbacks.Stop(ctx, profile, id, time.Duration(req.PositionMS)*time.Millisecond)
	})
}

func (a *API) reportPlayback(w http.ResponseWriter, r *http.Request, report func(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	reach, err := report(r.Context(), sessionOf(r).Profile.ID, id)
	switch {
	case errors.Is(err, playback.ErrNoPlayback):
		writeProblem(w, a.logger, codeNotFound, "the playback has stopped, or lapsed")
	case err != nil:
		a.internal(w, r, err)
	default:
		writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]domain.Reach{"reach": reach})
	}
}

// fileTypes are the types of the files a library holds, which Go's own table lacks.
var fileTypes = map[string]string{
	".mkv": "video/x-matroska", ".mk3d": "video/x-matroska", ".webm": "video/webm", ".mp4": "video/mp4",
	".m4v": "video/x-m4v", ".mov": "video/quicktime", ".ts": "video/mp2t", ".m2ts": "video/mp2t",
	".mts": "video/mp2t", ".avi": "video/x-msvideo", ".wmv": "video/x-ms-wmv", ".mpg": "video/mpeg",
	".mpeg": "video/mpeg", ".ogv": "video/ogg", ".flv": "video/x-flv",
	".srt": "application/x-subrip", ".vtt": "text/vtt", ".ass": "text/x-ssa", ".ssa": "text/x-ssa",
}

// partStream serves one file of a copy as it is, in byte ranges.
func (a *API) partStream(w http.ResponseWriter, r *http.Request) {
	a.serveLibraryFile(w, r, a.svc.Playing.PartFile)
}

// subtitleFile serves a subtitle file beside a copy as it is.
func (a *API) subtitleFile(w http.ResponseWriter, r *http.Request) {
	a.serveLibraryFile(w, r, a.svc.Playing.SubtitleFile)
}

// serveLibraryFile serves the file of a library that where finds for the id in the path, through
// the library's root, so a path can never leave it.
func (a *API) serveLibraryFile(w http.ResponseWriter, r *http.Request, where func(context.Context, uuid.UUID) (string, string, error)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	root, rel, err := where(r.Context(), id)
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
	if t, ok := fileTypes[strings.ToLower(path.Ext(rel))]; ok {
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
