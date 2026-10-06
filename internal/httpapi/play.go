package httpapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// streamFor is how long a stream's address stays good: longer than anyone watches in a sitting.
const streamFor = 24 * time.Hour

type playbacks interface {
	Start(ctx context.Context, method domain.PlayMethod, card domain.PlaybackCard) (domain.Playback, error)
	Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState) (domain.Reach, error)
	Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error)
	End(ctx context.Context, id uuid.UUID) error
	Abandon(ctx context.Context, id uuid.UUID) error
}

type remuxing interface {
	Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan) error
}

// owners say which node of the cluster serves a playback's HLS.
type owners interface {
	Owner(ctx context.Context, playback uuid.UUID) (string, bool, error)
}

type hlsFiles interface {
	Has(playback uuid.UUID) bool
	Playlist(playback uuid.UUID, name string) (string, error)
	SubtitleSegment(ctx context.Context, playback uuid.UUID, track, n int) (string, error)
	Init(ctx context.Context, playback uuid.UUID, part int) (*os.File, error)
	Segment(ctx context.Context, playback uuid.UUID, n int) (*os.File, error)
	Transcodes() (active, conversions, limit int)
	Encoder(video domain.VideoPlan) domain.Acceleration
	WebVTT(ctx context.Context, open func() (*os.File, error), language string) (string, error)
}

type playing interface {
	Playable(ctx context.Context, profile, item, version uuid.UUID) (store.PlayCopy, error)
	Card(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	VisiblePartFile(ctx context.Context, profile, part uuid.UUID) (root, rel string, err error)
	SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error)
	Subtitle(ctx context.Context, id uuid.UUID) (store.PlaySubtitle, error)
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

func decisions() []decision {
	return []decision{decisionCopy, decisionTranscode}
}

type playJSON struct {
	VersionID   string `json:"version_id,omitzero"`
	AudioStream *int   `json:"audio_stream,omitzero"`
	// SubtitleStream is a subtitle the client will show; one that is a picture is drawn into the
	// video where the client cannot draw it.
	SubtitleStream *int              `json:"subtitle_stream,omitzero"`
	Profile        *playback.Profile `json:"profile"`
}

// playbackJSON is a playback opened: its parts and subtitles played as they are, or its playlist,
// at addresses relative to the server.
type playbackJSON struct {
	PlaybackID uuid.UUID                `json:"playback_id"`
	Method     domain.PlayMethod        `json:"method"`
	VersionID  uuid.UUID                `json:"version_id"`
	Video      *videoJSON               `json:"video,omitzero"`
	Audio      *audioJSON               `json:"audio,omitzero"`
	Reasons    []domain.TranscodeReason `json:"reasons,omitzero"`
	Parts      []partJSON               `json:"parts,omitzero"`
	Subtitles  []subtitleJSON           `json:"subtitles,omitzero"`
	Playlist   string                   `json:"playlist,omitzero"`
	ExpiresAt  time.Time                `json:"expires_at"`
}

// refusalJSON is a copy nothing the client plays can be made of, and why.
type refusalJSON struct {
	problem
	Reasons []domain.TranscodeReason `json:"reasons"`
}

// play opens a playback of a film or episode as the client's profile decides: its copy's files in
// order, each with where it starts on the copy's timeline, or an HLS playlist of them, its video
// copied or encoded, at signed
// addresses a player fetches directly. It says what becomes of each stream, and why the copy could
// not be played as it is.
func (a *API) play(w http.ResponseWriter, r *http.Request) {
	var req playJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Profile == nil {
		writeProblem(w, a.logger, codeInvalidBody, "profile says what the client plays")
		return
	}
	if req.Profile.Parts == "" {
		req.Profile.Parts = domain.PartsJoined
	}
	if !slices.Contains(domain.PartPlaybacks(), req.Profile.Parts) {
		writeProblem(w, a.logger, codeInvalidBody, fmt.Sprintf("profile.parts is one of %v", domain.PartPlaybacks()))
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
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, version)
	if a.answered(w, r, err) {
		return
	}
	d, err := playback.Decide(*req.Profile, playback.Copy{Container: c.Container, BitrateKbps: c.BitrateKbps, Parts: len(c.Parts), Streams: c.Streams}, req.AudioStream, req.SubtitleStream)
	switch {
	case errors.Is(err, playback.ErrNoSuchAudio):
		writeProblem(w, a.logger, codeInvalidBody, "audio_stream is not one of the copy's audio streams")
		return
	case errors.Is(err, playback.ErrNoSuchSubtitle):
		writeProblem(w, a.logger, codeInvalidBody, "subtitle_stream is not one of the copy's subtitle streams")
		return
	case errors.Is(err, playback.ErrNoCompatibleStream):
		status := codeNoCompatibleStream.status()
		writeJSON(w, a.logger, "application/problem+json", status, refusalJSON{problem{Title: http.StatusText(status), Status: status, Code: codeNoCompatibleStream}, d.Reasons})
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	title, err := a.svc.Playing.Card(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	session, err := a.svc.Playbacks.Start(r.Context(), d.Method, a.cardOf(r, title, c, d, req.SubtitleStream))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	until := time.Now().Add(streamFor)
	answer := playbackJSON{
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
			if aerr := a.svc.Playbacks.Abandon(context.WithoutCancel(r.Context()), session.ID); aerr != nil {
				a.internal(w, r, aerr)
				return
			}
			if errors.Is(err, hls.ErrTranscodeLimit) {
				_, _, limit := a.svc.HLS.Transcodes()
				writeProblem(w, a.logger, codeTranscodeLimit, fmt.Sprintf("the server is already transcoding as many videos at once as it may: %d", limit))
				return
			}
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
			answer.Subtitles = append(answer.Subtitles, subtitleJSON{
				ID: f.ID, Codec: f.Codec, Language: tagOf(f.Language), Title: f.Title, Default: f.Default, Forced: f.Forced,
				HearingImpaired: f.HearingImpaired, URL: a.svc.Signer.Sign("/api/v1/subtitles/"+f.ID.String()+"/file", until),
			})
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, answer)
}

// cardOf is what the dashboard shows of a playback the request starts of a copy, as decided.
func (a *API) cardOf(r *http.Request, t store.Card, c store.PlayCopy, d playback.Decision, subtitle *int) domain.PlaybackCard {
	s := sessionOf(r)
	card := domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: s.Profile.ID, Name: s.Profile.Name},
		Device: domain.PlaybackDevice{
			ID: s.ID, Name: s.Device, Client: s.Client, Address: clientAddr(r, a.svc.TrustedProxies).String(),
		},
		Title: domain.PlaybackTitle{
			ID: t.ID, Kind: t.Kind, Title: t.Title, Year: t.Year, SeasonNumber: t.SeasonNumber,
			EpisodeNumber: t.EpisodeNumber, EpisodeEnd: t.EpisodeEnd, Poster: t.Poster, Thumb: t.Thumb, Backdrop: t.Backdrop,
		},
		Version: domain.PlaybackVersion{
			ID: c.Version, Edition: c.Edition, Label: c.Label, Container: c.Container, BitrateKbps: c.BitrateKbps,
			DurationMS: c.DurationMS,
		},
		Reasons: d.Reasons,
	}
	if t.Show != nil {
		card.Title.ShowID, card.Title.Show = t.Show.ID, t.Show.Title
	}
	stream := func(index int) media.Stream {
		if i := slices.IndexFunc(c.Streams, func(s media.Stream) bool { return s.Index == index }); i >= 0 {
			return c.Streams[i]
		}
		return media.Stream{Index: index}
	}
	if v := d.Video; v != nil {
		src := stream(v.Stream)
		card.Video = &domain.PlaybackVideo{
			Stream: v.Stream, Codec: src.Codec, Profile: src.Profile, Width: src.Width, Height: src.Height,
			Range: src.Range, BitrateKbps: src.BitrateKbps, DolbyVision: v.DolbyVision,
		}
		if e := v.Encode; e != nil {
			card.Video.Encode = &domain.PlaybackEncode{
				Codec: e.Codec, Width: e.Width, Height: e.Height, BitrateKbps: e.BitrateKbps, ToneMapped: e.ToneMap,
			}
			card.Acceleration = a.svc.HLS.Encoder(*v)
		}
	}
	if au := d.Audio; au != nil {
		src := stream(au.Stream)
		card.Audio = &domain.PlaybackAudio{
			Stream: au.Stream, Codec: src.Codec, Language: tagOf(src.Language), Channels: src.Channels,
			BitrateKbps: src.BitrateKbps,
		}
		if e := au.Encode; e != nil {
			card.Audio.Encode = &domain.PlaybackEncode{Codec: e.Codec, Channels: e.Channels, BitrateKbps: e.BitrateKbps}
		}
	}
	if subtitle != nil {
		src := stream(*subtitle)
		card.Subtitle = &domain.PlaybackSubtitle{
			Stream: *subtitle, Codec: src.Codec, Language: tagOf(src.Language),
			Burned: d.Video != nil && d.Video.Encode != nil && d.Video.Encode.Burn != nil,
		}
	}
	return card
}

// tagOf is a language's BCP 47 tag, or nothing for none.
func tagOf(l language.Tag) string {
	if l == language.Und {
		return ""
	}
	return l.String()
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

// routeToOwner hands a request about the playback the path names by param that another node of
// the cluster runs to that node, which checks the request again: its HLS, whose signature every
// node makes with the server's one key, or its player's or an admin's stop of it, so its stream
// ends and its transcode slot is free at once.
func (a *API) routeToOwner(param string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playback, err := uuid.Parse(r.PathValue(param))
		if err != nil || a.svc.Owners == nil || a.svc.HLS.Has(playback) {
			next.ServeHTTP(w, r)
			return
		}
		address, elsewhere, err := a.svc.Owners.Owner(r.Context(), playback)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		target, perr := url.Parse(address)
		if !elsewhere || perr != nil {
			next.ServeHTTP(w, r)
			return
		}
		a.proxy(w, r, target)
	})
}

// proxy hands a request to another node of the cluster.
func (a *API) proxy(w http.ResponseWriter, r *http.Request, target *url.URL) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		ErrorLog: slog.NewLogLogger(a.logger.Handler(), slog.LevelWarn),
	}
	proxy.ServeHTTP(w, r)
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

type playbackProgressJSON struct {
	PositionMS int64            `json:"position_ms"`
	State      domain.PlayState `json:"state"`
}

// playbackProgress is the player saying where it has got to, every ten seconds or so.
func (a *API) playbackProgress(w http.ResponseWriter, r *http.Request) {
	var req playbackProgressJSON
	if !a.decode(w, r, &req) {
		return
	}
	state, ok := domain.ParsePlayState(string(req.State))
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
	var req positionJSON
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
		writeJSON(w, a.logger, "application/json", http.StatusOK, reachedJSON{Reach: reach})
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
	a.serveLibraryFile(w, r, a.svc.Playing.PartFile, math.MaxInt64)
}

// sampleBytes is as much of a part as a connection test may read: enough to time a fast link,
// too little to stand in for a download.
const sampleBytes = 16 << 20

// partSample serves the start of a part's file to time the connection, as Jellyfin's bitrate test
// does, but of the file itself. It is no playback: nothing is recorded of it.
func (a *API) partSample(w http.ResponseWriter, r *http.Request) {
	visible := func(ctx context.Context, part uuid.UUID) (string, string, error) {
		return a.svc.Playing.VisiblePartFile(ctx, sessionOf(r).Profile.ID, part)
	}
	a.serveLibraryFile(w, r, visible, sampleBytes)
}

// subtitleFormat is how a subtitle file beside a copy is served.
type subtitleFormat string

const (
	subtitleOriginal subtitleFormat = "original"
	subtitleWebVTT   subtitleFormat = "webvtt"
)

func subtitleFormats() []subtitleFormat { return []subtitleFormat{subtitleOriginal, subtitleWebVTT} }

// subtitleFile serves a subtitle file beside a copy as it is, or a text one converted to WebVTT,
// as Jellyfin's subtitle route converts, for a player that draws nothing else.
func (a *API) subtitleFile(w http.ResponseWriter, r *http.Request) {
	format := subtitleFormat(cmp.Or(r.URL.Query().Get("format"), string(subtitleOriginal)))
	if !slices.Contains(subtitleFormats(), format) {
		writeProblem(w, a.logger, codeInvalidParameter, fmt.Sprintf("format is one of %v", subtitleFormats()))
		return
	}
	switch format {
	case subtitleOriginal:
		a.serveLibraryFile(w, r, a.svc.Playing.SubtitleFile, math.MaxInt64)
	case subtitleWebVTT:
		a.subtitleVTT(w, r)
	}
}

func (a *API) subtitleVTT(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	sub, err := a.svc.Playing.Subtitle(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if !hls.TextSubtitle(sub.Codec) {
		writeProblem(w, a.logger, codeInvalidParameter, "format webvtt is for text subtitles, and this one is pictures")
		return
	}
	ctx := r.Context()
	open := func() (*os.File, error) {
		f, _, err := openLibraryFile(ctx, a.svc.Playing.SubtitleFile, id)
		return f, err
	}
	vtt, err := a.svc.HLS.WebVTT(ctx, open, tagOf(sub.Language))
	if errors.Is(err, fs.ErrNotExist) {
		err = store.ErrNotFound
	}
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = io.WriteString(w, vtt)
}

// serveLibraryFile serves the first limit bytes of the file of a library that where finds for the
// id in the path: only a file the scanner recorded.
func (a *API) serveLibraryFile(w http.ResponseWriter, r *http.Request, where func(context.Context, uuid.UUID) (string, string, error), limit int64) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	f, rel, err := openLibraryFile(r.Context(), where, id)
	if errors.Is(err, fs.ErrNotExist) {
		err = store.ErrNotFound
	}
	if a.answered(w, r, err) {
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
	http.ServeContent(w, r, rel, info.ModTime(), io.NewSectionReader(f, 0, min(info.Size(), limit)))
}

// openLibraryFile opens the file of a library that where finds for an id.
func openLibraryFile(ctx context.Context, where func(context.Context, uuid.UUID) (string, string, error), id uuid.UUID) (*os.File, string, error) {
	root, rel, err := where(ctx, id)
	if err != nil {
		return nil, "", err
	}
	f, err := library.Open(root, rel)
	return f, rel, err
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
