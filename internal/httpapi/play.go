package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
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
	Opened(ctx context.Context, p domain.Playback)
	Serve(ctx context.Context, id uuid.UUID, cut func()) (done func(), err error)
}

// placer chooses the node that encodes a playback, and opens its remux there.
type placer interface {
	Candidates(ctx context.Context, need playback.Need) ([]domain.Node, error)
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o playback.Opening) error
	Self() domain.Node
	Refuse(candidates []domain.Node) error
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
	ID              uuid.UUID    `json:"id,omitzero"`
	Stream          *int         `json:"stream,omitzero"`
	Codec           string       `json:"codec"`
	Kind            subtitleKind `json:"kind"`
	Language        string       `json:"language,omitzero"`
	Title           string       `json:"title,omitzero"`
	Default         bool         `json:"default,omitzero"`
	Forced          bool         `json:"forced,omitzero"`
	HearingImpaired bool         `json:"hearing_impaired,omitzero"`
	URL             string       `json:"url"`
	Fonts           string       `json:"fonts,omitzero"`
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
// copied or encoded, at signed addresses a player fetches directly. It says what becomes of each
// stream, and why the copy could not be played as it is.
func (a *API) play(w http.ResponseWriter, r *http.Request) {
	req, version, ok := a.playRequest(w, r)
	if !ok {
		return
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
	candidates, err := a.svc.Placer.Candidates(r.Context(), playback.Need{})
	if a.answered(w, r, err) {
		return
	}
	title, err := a.svc.Playing.PlaybackTitle(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	d, session, err := a.place(r, req, id, c, title, candidates)
	if errors.Is(err, playback.ErrNoCompatibleStream) {
		status := codeNoCompatibleStream.status()
		writeJSON(w, a.logger, "application/problem+json", status, refusalJSON{problem{Title: http.StatusText(status), Status: status, Code: codeNoCompatibleStream}, d.Reasons})
		return
	}
	if errors.Is(err, hls.ErrTranscodeLimit) {
		writeProblem(w, a.logger, codeTranscodeLimit, a.svc.Placer.Refuse(candidates).Error())
		return
	}
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.opened(*req.Profile, c, d, session))
}

// playRequest is a play's body, checked, with the parts and segments the client left out filled
// in, and the copy it names.
func (a *API) playRequest(w http.ResponseWriter, r *http.Request) (playJSON, uuid.UUID, bool) {
	var req playJSON
	if !a.decode(w, r, &req) {
		return req, uuid.UUID{}, false
	}
	if req.Profile == nil {
		writeProblem(w, a.logger, codeInvalidBody, "profile says what the client plays")
		return req, uuid.UUID{}, false
	}
	if req.StartMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "start_ms is not negative")
		return req, uuid.UUID{}, false
	}
	if req.SubtitleStream != nil && req.SubtitleFile != nil {
		writeProblem(w, a.logger, codeInvalidBody, "a subtitle is subtitle_stream or subtitle_file, not both")
		return req, uuid.UUID{}, false
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
			return req, uuid.UUID{}, false
		}
	}
	return req, version, true
}

// place decides how c is played and starts its playback. The node with the most transcode slots
// free decides, with what it encodes; the next is asked where it has none by then, and decides
// again with its own. Where no node takes video to encode, this one decides, and a copy it would
// encode is refused.
func (a *API) place(r *http.Request, req playJSON, id uuid.UUID, c store.PlayCopy, title domain.PlaybackTitle, candidates []domain.Node) (playback.Decision, domain.Playback, error) {
	tracks := domain.ChosenTracks{Audio: req.AudioStream, Subtitle: req.SubtitleStream, SubtitleFile: req.SubtitleFile}
	asked := candidates
	if len(asked) == 0 {
		asked = []domain.Node{a.svc.Placer.Self()}
	}
	var d playback.Decision
	var session domain.Playback
	var err error
	for _, node := range asked {
		d, err = playback.Decide(*req.Profile, playback.CopyOf(c), tracks, playback.EncodingOf(node))
		if err != nil {
			break
		}
		if d.Video == nil || d.Video.Encode == nil {
			// Nothing is encoded: the node asked plays it.
			node = a.svc.Placer.Self()
		} else if len(candidates) == 0 {
			err = hls.ErrTranscodeLimit
			break
		}
		session, err = a.start(r, node, title, c, d, tracks, playback.Opening{
			Profile: sessionOf(r).Profile.ID, Item: id, Version: c.Version, Segments: req.Profile.Segments, StartMS: req.StartMS,
		})
		if !errors.Is(err, hls.ErrTranscodeLimit) {
			break
		}
	}
	return d, session, err
}

// opened is what the client is told of a playback of c, played as d decided.
func (a *API) opened(p playback.Profile, c store.PlayCopy, d playback.Decision, session domain.Playback) playbackJSON {
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
		for _, part := range c.Parts {
			answer.Parts = append(answer.Parts, partJSON{
				ID: part.ID, URL: a.svc.Signer.Sign("/api/v1/playbacks/"+session.ID.String()+"/parts/"+part.ID.String()+"/stream", until),
				OffsetMS: part.OffsetMS, DurationMS: part.DurationMS,
			})
		}
	}
	if v := d.Video; v == nil || v.Encode == nil || v.Encode.Burn == nil && v.Encode.BurnFile == nil {
		answer.Subtitles = a.sidecars(p, c, d.Method, until)
	}
	return answer
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
			Stream: &s.Index, Codec: s.Codec, Kind: subtitleKindOf(s.Codec), Language: domain.TagOf(s.Language), Title: s.Title, Default: s.Default,
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
			ID: f.ID, Codec: f.Codec, Kind: subtitleKindOf(f.Codec), Language: domain.TagOf(f.Language), Title: f.Title, Default: f.Default, Forced: f.Forced,
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
	if err != nil {
		return session, err
	}
	if d.Method != domain.PlayDirect {
		o.Video, o.Audio = *d.Video, d.Audio
		if err := a.svc.Placer.Open(r.Context(), node, session.ID, c, o); err != nil {
			if aerr := a.svc.Playbacks.Abandon(context.WithoutCancel(r.Context()), session.ID); aerr != nil {
				return session, errors.Join(err, aerr)
			}
			return session, err
		}
	}
	a.svc.Playbacks.Opened(r.Context(), session)
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
	// A node set to serve only, or drained, since another placed this on it, takes nothing new.
	if self := a.svc.Placer.Self(); !self.Role.Encodes() || !self.Availability.Takes() {
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

func (a *API) playRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/titles/{id}/play", access: signedIn,
			summary: "Open a playback of a film or episode, as the client's profile can play it",
			body:    playJSON{}, status: http.StatusOK, reply: playbackJSON{},
			refusals: map[int]any{codeNoCompatibleStream.status(): refusalJSON{}}, handle: a.play,
		},
		{
			pattern: "POST /api/v1/playbacks/{id}/progress", access: signedIn, summary: "Say where a playback has got to, paused too: one unheard from for two minutes is stopped",
			body: playbackProgressJSON{}, status: http.StatusOK, reply: reachedJSON{}, handle: a.playbackProgress,
		},
		{
			pattern: "POST /api/v1/playbacks/{id}/stop", access: signedIn, summary: "Stop a playback, and say where",
			body: positionJSON{}, status: http.StatusOK, reply: reachedJSON{},
			handle: a.routeToOwner("id", http.HandlerFunc(a.playbackStop)).ServeHTTP,
		},
	}
}
