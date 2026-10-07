package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// streamFor is how long a stream's address stays good: longer than anyone watches in a sitting.
const streamFor = 24 * time.Hour

type playbacks interface {
	Start(ctx context.Context, id uuid.UUID, method domain.PlayMethod, card domain.PlaybackCard, node uuid.UUID) (domain.Playback, error)
	Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState, tracks domain.ChosenTracks) (domain.Reach, error)
	Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error)
	End(ctx context.Context, id uuid.UUID) error
	Abandon(ctx context.Context, id uuid.UUID) error
	Serve(ctx context.Context, id uuid.UUID, cut func()) (done func(), err error)
}

// placer chooses the node that encodes a playback, and opens its remux there.
type placer interface {
	Candidates(ctx context.Context, need playback.Need) ([]domain.Node, error)
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o playback.Opening) error
	Self() domain.Node
}

// owners say which node of the cluster serves a playback's HLS.
type owners interface {
	Owner(ctx context.Context, playback uuid.UUID) (string, bool, error)
}

type hlsFiles interface {
	Has(playback uuid.UUID) bool
	Resource(ctx context.Context, playback uuid.UUID, name string) (hls.Resource, error)
	Transcodes() (active, conversions, limit int)
	Encoder(video domain.VideoPlan) domain.Acceleration
	WebVTT(ctx context.Context, open func() (*os.File, error), language string) (string, error)
	Extracted(ctx context.Context, src hls.SubtitleSource, want string) (string, error)
}

type playing interface {
	Playable(ctx context.Context, profile, item, version uuid.UUID) (store.PlayCopy, error)
	PlaybackTitle(ctx context.Context, id uuid.UUID) (domain.PlaybackTitle, error)
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	VisiblePartFile(ctx context.Context, profile, part uuid.UUID) (root, rel string, err error)
	SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error)
	Subtitle(ctx context.Context, id uuid.UUID) (store.PlaySubtitle, error)
	PartStreams(ctx context.Context, part uuid.UUID) ([]domain.Stream, error)
}

type partJSON struct {
	ID         uuid.UUID `json:"id"`
	URL        string    `json:"url"`
	OffsetMS   int64     `json:"offset_ms"`
	DurationMS int64     `json:"duration_ms"`
}

// subtitleJSON is a subtitle the client draws beside the video, timed on the copy's timeline: a
// file beside the copy, by its id, or a styled stream of it read out as it is, by its index, with
// the address of the fonts the file carries for it.
type subtitleJSON struct {
	ID              uuid.UUID `json:"id,omitzero"`
	Stream          *int      `json:"stream,omitzero"`
	Codec           string    `json:"codec"`
	Language        string    `json:"language,omitzero"`
	Title           string    `json:"title,omitzero"`
	Default         bool      `json:"default,omitzero"`
	Forced          bool      `json:"forced,omitzero"`
	HearingImpaired bool      `json:"hearing_impaired,omitzero"`
	URL             string    `json:"url"`
	Fonts           string    `json:"fonts,omitzero"`
}

type videoJSON struct {
	Stream      int                        `json:"stream"`
	Decision    decision                   `json:"decision"`
	DolbyVision domain.DolbyVisionHandling `json:"dolby_vision,omitzero"`
	Codec       domain.VideoCodec          `json:"codec,omitzero"`
	Width       int                        `json:"width,omitzero"`
	Height      int                        `json:"height,omitzero"`
	BitrateKbps int                        `json:"bitrate_kbps,omitzero"`
	// Range is the range it is encoded in: SDR, or the copy's HDR kept; tone_mapped says HDR was
	// mapped to SDR to reach it.
	Range      domain.Range `json:"range,omitzero"`
	ToneMapped bool         `json:"tone_mapped,omitzero"`
	// BurnedSubtitle is the subtitle stream drawn into the picture, BurnedSubtitleFile the
	// subtitle file beside the copy drawn so.
	BurnedSubtitle     *int       `json:"burned_subtitle,omitzero"`
	BurnedSubtitleFile *uuid.UUID `json:"burned_subtitle_file,omitzero"`
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
	// SubtitleStream or SubtitleFile is a subtitle the client will show, a stream of the copy or a
	// text file beside it; one that is a picture or styled is drawn into the video where the
	// client cannot draw it.
	SubtitleStream *int              `json:"subtitle_stream,omitzero"`
	SubtitleFile   *uuid.UUID        `json:"subtitle_file,omitzero"`
	Profile        *playback.Profile `json:"profile"`
	// StartMS is where on the copy's timeline the player starts, so an HLS playlist is made from
	// there before the player asks for it.
	StartMS int64 `json:"start_ms,omitzero"`
}

// playbackJSON is a playback opened: its parts played as they are, or its playlist, and the
// subtitles the client draws beside them, at addresses relative to the server.
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
	if req.StartMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "start_ms is not negative")
		return
	}
	if req.SubtitleStream != nil && req.SubtitleFile != nil {
		writeProblem(w, a.logger, codeInvalidBody, "a subtitle is subtitle_stream or subtitle_file, not both")
		return
	}
	if req.Profile.Parts == "" {
		req.Profile.Parts = domain.PartsJoined
	}
	if req.Profile.Segments == "" {
		req.Profile.Segments = domain.SegmentsFMP4
	}
	var version uuid.UUID
	if req.VersionID != "" {
		var err error
		if version, err = uuid.Parse(req.VersionID); err != nil {
			writeProblem(w, a.logger, codeInvalidBody, "version_id is not an id")
			return
		}
	}
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, version)
	if a.answered(w, r, err) {
		return
	}
	if req.AudioStream == nil {
		prefs, last, err := a.playingAs(r.Context(), sessionOf(r).Profile.ID, id)
		if a.answered(w, r, err) {
			return
		}
		audio, subtitles := copyTracks(c)
		req.AudioStream = playback.DefaultTracks(audio, subtitles, prefs, last).Audio
	}
	limit, err := playback.RemoteLimit(r.Context(), a.svc.Network, a.svc.TrustedProxies.Client(r))
	if a.answered(w, r, err) {
		return
	}
	req.Profile.MaxBitrateKbps = playback.Capped(req.Profile.MaxBitrateKbps, limit)
	tracks := domain.ChosenTracks{Audio: req.AudioStream, Subtitle: req.SubtitleStream, SubtitleFile: req.SubtitleFile}
	candidates, err := a.svc.Placer.Candidates(r.Context(), playback.Need{})
	if a.answered(w, r, err) {
		return
	}
	title, err := a.svc.Playing.PlaybackTitle(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	// The node with the most transcode slots free decides how the copy is played, with what it
	// encodes; the next is asked where it has none by then, and decides again with its own.
	var d playback.Decision
	var session domain.Playback
	for _, node := range candidates {
		d, err = playback.Decide(*req.Profile, playback.CopyOf(c), tracks, playback.EncodingOf(node))
		if err != nil {
			break
		}
		if d.Video == nil || d.Video.Encode == nil {
			// Nothing is encoded: the node asked plays it.
			node = a.svc.Placer.Self()
		}
		session, err = a.start(r, node, title, c, d, tracks, playback.Opening{
			Profile: sessionOf(r).Profile.ID, Item: id, Version: c.Version, Segments: req.Profile.Segments, StartMS: req.StartMS,
		})
		if !errors.Is(err, hls.ErrTranscodeLimit) {
			break
		}
	}
	if errors.Is(err, playback.ErrNoCompatibleStream) {
		status := codeNoCompatibleStream.status()
		writeJSON(w, a.logger, "application/problem+json", status, refusalJSON{problem{Title: http.StatusText(status), Status: status, Code: codeNoCompatibleStream}, d.Reasons})
		return
	}
	if errors.Is(err, hls.ErrTranscodeLimit) {
		writeProblem(w, a.logger, codeTranscodeLimit, playback.Full(candidates).Error())
		return
	}
	if a.answered(w, r, err) {
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
				BitrateKbps: e.BitrateKbps, Range: e.Range, ToneMapped: e.ToneMap, BurnedSubtitle: e.Burn,
				BurnedSubtitleFile: e.BurnFile,
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
		subject := hlsSubject(session.ID)
		exp, sig := a.svc.Signer.Token(subject, until)
		answer.Playlist = subject + "/" + exp + "/" + sig + "/main.m3u8"
	} else {
		for _, p := range c.Parts {
			answer.Parts = append(answer.Parts, partJSON{
				ID: p.ID, URL: a.svc.Signer.Sign("/api/v1/playbacks/"+session.ID.String()+"/parts/"+p.ID.String()+"/stream", until),
				OffsetMS: p.OffsetMS, DurationMS: p.DurationMS,
			})
		}
	}
	if v := d.Video; v == nil || v.Encode == nil || v.Encode.Burn == nil && v.Encode.BurnFile == nil {
		answer.Subtitles = a.sidecars(*req.Profile, c, d.Method, until)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, answer)
}

// sidecars are the subtitles a playback hands the client to draw beside its video, but none
// beside one drawn in: the files beside the copy it draws so, but plain ones HLS carries itself,
// and the styled streams of a copy in one file, read out as they are, but those it draws from the
// file it plays.
func (a *API) sidecars(p playback.Profile, c store.PlayCopy, method domain.PlayMethod, until time.Time) []subtitleJSON {
	var out []subtitleJSON
	for _, s := range c.Streams {
		if s.Kind != domain.StreamSubtitle || !p.Sidecar(s.Codec, false, len(c.Parts)) ||
			method == domain.PlayDirect && p.Draws(s.Codec, domain.SubtitleEmbedded) {
			continue
		}
		part := "/api/v1/parts/" + c.Parts[0].ID.String()
		out = append(out, subtitleJSON{
			Stream: &s.Index, Codec: s.Codec, Language: domain.TagOf(s.Language), Title: s.Title, Default: s.Default,
			Forced: s.Forced, HearingImpaired: s.HearingImpaired,
			URL:   a.svc.Signer.Sign(part+"/subtitles/"+strconv.Itoa(s.Index), until),
			Fonts: a.svc.Signer.Sign(part+"/fonts", until),
		})
	}
	for _, f := range c.Subtitles {
		if !p.Sidecar(f.Codec, true, len(c.Parts)) || method != domain.PlayDirect && hls.TextSubtitle(f.Codec) {
			continue
		}
		out = append(out, subtitleJSON{
			ID: f.ID, Codec: f.Codec, Language: domain.TagOf(f.Language), Title: f.Title, Default: f.Default, Forced: f.Forced,
			HearingImpaired: f.HearingImpaired, URL: a.svc.Signer.Sign("/api/v1/subtitles/"+f.ID.String()+"/file", until),
		})
	}
	return out
}

// playbackCard is what the dashboard shows of a playback the request starts.
func (a *API) playbackCard(r *http.Request, node domain.Node, t domain.PlaybackTitle, c store.PlayCopy, d playback.Decision, tracks domain.ChosenTracks) domain.PlaybackCard {
	card := playback.Card(sessionOf(r), a.svc.TrustedProxies.Client(r).String(), t, c, d, tracks)
	if v := d.Video; v != nil && v.Encode != nil {
		card.Acceleration = hls.EncodedOn(node.Encoder.Acceleration, *v)
	}
	return card
}

// start starts a playback of c that node serves, and its remux there where it is not played
// directly; a node with every slot held refuses it (hls.ErrTranscodeLimit), and it is forgotten.
func (a *API) start(r *http.Request, node domain.Node, t domain.PlaybackTitle, c store.PlayCopy, d playback.Decision, tracks domain.ChosenTracks, o playback.Opening) (domain.Playback, error) {
	session, err := a.svc.Playbacks.Start(r.Context(), uuid.NewV7(), d.Method, a.playbackCard(r, node, t, c, d, tracks), node.ID)
	if err != nil || d.Method == domain.PlayDirect {
		return session, err
	}
	o.Video, o.Audio = *d.Video, d.Audio
	if err := a.svc.Placer.Open(r.Context(), node, session.ID, c, o); err != nil {
		if aerr := a.svc.Playbacks.Abandon(context.WithoutCancel(r.Context()), session.ID); aerr != nil {
			return session, errors.Join(err, aerr)
		}
		return session, err
	}
	return session, nil
}

// openRemote opens the remux of a playback another node placed on this one, reading its copy
// again as the profile may see it: 204 once open, 503 where every transcode slot is held.
func (a *API) openRemote(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	var o playback.Opening
	if err == nil {
		err = json.NewDecoder(r.Body).Decode(&o)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// A node set to serve only, since another placed this on it, encodes nothing.
	if !a.svc.Placer.Self().Role.Encodes() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	c, err := a.svc.Playing.Playable(r.Context(), o.Profile, o.Item, o.Version)
	if err == nil {
		err = a.svc.Placer.Open(r.Context(), a.svc.Placer.Self(), id, c, o)
	}
	switch {
	case errors.Is(err, hls.ErrTranscodeLimit):
		w.WriteHeader(http.StatusServiceUnavailable)
	case err != nil:
		a.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func hlsSubject(playback uuid.UUID) string { return "/api/v1/hls/" + playback.String() }

// hlsFile serves a remux's playlist, a part's initialisation or a segment, made as they are asked
// for. The playlist addresses everything else relative to itself, so one signature covers it all.
func (a *API) hlsFile(w http.ResponseWriter, r *http.Request) {
	playback, ok := a.pathID(w, r, "playback")
	if !ok {
		return
	}
	name := r.PathValue("file")
	res, err := a.svc.HLS.Resource(r.Context(), playback, name)
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", res.Type)
	if res.File == nil {
		_, _ = io.WriteString(w, res.Text)
		return
	}
	a.serveFile(w, r, res.File, name, nil)
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
	// AudioStream and SubtitleStream or SubtitleFile are the tracks playing, kept for the title
	// where the profile remembers them; subtitle_stream -1 is none.
	AudioStream    *int       `json:"audio_stream,omitzero"`
	SubtitleStream *int       `json:"subtitle_stream,omitzero"`
	SubtitleFile   *uuid.UUID `json:"subtitle_file,omitzero"`
}

// playbackProgress is the player saying where it has got to, every ten seconds or so.
func (a *API) playbackProgress(w http.ResponseWriter, r *http.Request) {
	var req playbackProgressJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.State == "" || req.PositionMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "position_ms is not negative and state is playing or paused")
		return
	}
	if req.AudioStream != nil && *req.AudioStream < 0 || req.SubtitleStream != nil && *req.SubtitleStream < domain.NoSubtitle ||
		req.SubtitleStream != nil && req.SubtitleFile != nil {
		writeProblem(w, a.logger, codeInvalidBody, "audio_stream is a stream, and subtitles are subtitle_stream, -1 for none, or subtitle_file")
		return
	}
	tracks := domain.ChosenTracks{Audio: req.AudioStream, Subtitle: req.SubtitleStream, SubtitleFile: req.SubtitleFile}
	a.reportPlayback(w, r, func(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error) {
		return a.svc.Playbacks.Progress(ctx, profile, id, time.Duration(req.PositionMS)*time.Millisecond, req.State, tracks)
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
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	reach, err := report(r.Context(), sessionOf(r).Profile.ID, id)
	if !a.answered(w, r, err) {
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

// playbackPartStream serves a copy's file as it is to its playback, for as long as the playback
// lasts: its stop cuts off what is still being sent.
func (a *API) playbackPartStream(w http.ResponseWriter, r *http.Request) {
	playback, ok := a.pathID(w, r, "playback")
	if !ok {
		return
	}
	rc := http.NewResponseController(w)
	done, err := a.svc.Playbacks.Serve(r.Context(), playback, func() { _ = rc.SetWriteDeadline(time.Now()) })
	if a.answered(w, r, err) {
		return
	}
	defer done()
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
	format, ok := queryEnum(a, w, r, "format", subtitleOriginal, subtitleFormats())
	if !ok {
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
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	sub, err := a.svc.Playing.Subtitle(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if !hls.TextSubtitle(sub.Codec) {
		writeProblem(w, a.logger, codeInvalidParameter, "format webvtt is for plain text subtitles: WebVTT carries no pictures, and would lose a styled one's look")
		return
	}
	ctx := r.Context()
	open := func() (*os.File, error) {
		f, _, err := openLibraryFile(ctx, a.svc.Playing.SubtitleFile, id)
		return f, err
	}
	vtt, err := a.svc.HLS.WebVTT(ctx, open, domain.TagOf(sub.Language))
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = io.WriteString(w, vtt)
}

// serveLibraryFile serves the first limit bytes of the file of a library that where finds for the
// id in the path: only a file the scanner recorded.
func (a *API) serveLibraryFile(w http.ResponseWriter, r *http.Request, where func(context.Context, uuid.UUID) (string, string, error), limit int64) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	f, rel, err := openLibraryFile(r.Context(), where, id)
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
	if limit >= info.Size() {
		// The bare file keeps the copy in the kernel: sendfile takes only an *os.File.
		http.ServeContent(w, r, rel, info.ModTime(), f)
		return
	}
	http.ServeContent(w, r, rel, info.ModTime(), io.NewSectionReader(f, 0, limit))
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

// fontTypes are the types of the fonts a file carries for its styled subtitles.
var fontTypes = map[string]string{
	".ttf": "font/ttf", ".otf": "font/otf", ".ttc": "font/collection", ".woff": "font/woff", ".woff2": "font/woff2",
}

type fontsJSON struct {
	Fonts []fontJSON `json:"fonts"`
}

// fontJSON is a font a file carries, at an address signed as long as the list's own.
type fontJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// partSubtitles is a part's file, opened as the scanner recorded it, and its streams, for reading
// its subtitles out. The read outlives the request that starts it.
func (a *API) partSubtitles(ctx context.Context, part uuid.UUID) (hls.SubtitleSource, error) {
	streams, err := a.svc.Playing.PartStreams(ctx, part)
	if err != nil {
		return hls.SubtitleSource{}, err
	}
	opening := context.WithoutCancel(ctx)
	open := func() (*os.File, error) {
		f, _, err := openLibraryFile(opening, a.svc.Playing.PartFile, part)
		return f, err
	}
	return hls.SubtitleSource{Open: open, Part: part, Streams: streams}, nil
}

// styledStream serves a styled subtitle stream of a part, read out as it is.
func (a *API) styledStream(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("stream"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	if !slices.ContainsFunc(src.Streams, func(s domain.Stream) bool {
		return s.Index == n && s.Kind == domain.StreamSubtitle && hls.StyledSubtitle(s.Codec)
	}) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.StyledName(n))
	if a.answered(w, r, err) {
		return
	}
	f, err := os.Open(filepath.Join(dir, hls.StyledName(n)))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.serveFile(w, r, f, hls.StyledName(n), http.Header{"Content-Type": {"text/x-ssa; charset=utf-8"}})
}

// partFonts lists the fonts a part's file carries for its styled subtitles, each at an address
// signed until the list's own lapses.
func (a *API) partFonts(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.FontsDir)
	if a.answered(w, r, err) {
		return
	}
	entries, err := os.ReadDir(filepath.Join(dir, hls.FontsDir))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// requireSignature has read exp already.
	exp, _ := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
	answer := fontsJSON{Fonts: []fontJSON{}}
	for _, e := range entries {
		if _, ok := fontTypes[strings.ToLower(filepath.Ext(e.Name()))]; !ok || !e.Type().IsRegular() {
			continue
		}
		at := r.URL.Path + "/" + e.Name()
		expires, sig := a.svc.Signer.Token(at, time.Unix(exp, 0))
		u := url.URL{Path: at, RawQuery: url.Values{"exp": {expires}, "sig": {sig}}.Encode()}
		answer.Fonts = append(answer.Fonts, fontJSON{Name: e.Name(), URL: u.String()})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, answer)
}

// partFont serves a font a part's file carries, as partFonts listed it.
func (a *API) partFont(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	name := r.PathValue("name")
	t, ok := fontTypes[strings.ToLower(filepath.Ext(name))]
	if !ok {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.FontsDir)
	if a.answered(w, r, err) {
		return
	}
	f, err := os.OpenInRoot(filepath.Join(dir, hls.FontsDir), name)
	if errors.Is(err, fs.ErrNotExist) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// A font's bytes never change under its address: the part's file is read out once.
	a.serveFile(w, r, f, name, http.Header{"Content-Type": {t}, "Cache-Control": {"private, max-age=86400"}})
}
