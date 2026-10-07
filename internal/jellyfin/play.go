package jellyfin

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type playing interface {
	Playable(ctx context.Context, profile, item, version uuid.UUID) (store.PlayCopy, error)
	PlaybackTitle(ctx context.Context, id uuid.UUID) (domain.PlaybackTitle, error)
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error)
}

type playbacks interface {
	Start(ctx context.Context, id uuid.UUID, method domain.PlayMethod, card domain.PlaybackCard) (domain.Playback, error)
	Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState, tracks domain.ChosenTracks) (domain.Reach, error)
	Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error)
	Finish(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error)
	Abandon(ctx context.Context, id uuid.UUID) error
}

type watching interface {
	Length(ctx context.Context, item uuid.UUID) (time.Duration, error)
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position, length time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error)
	MarkWatched(ctx context.Context, profile, item uuid.UUID, at *time.Time) error
	MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error
	Favourite(ctx context.Context, profile, item uuid.UUID) error
	Unfavourite(ctx context.Context, profile, item uuid.UUID) error
}

// maxReport is the most a playback's report or request may be; a device profile is the largest.
const maxReport = 1 << 20

// readJSON reads a body an app may leave empty, its keys in any case, within bodyWithin.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyWithin))
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReport)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		refuse(w, http.StatusBadRequest)
		return false
	}
	return true
}

func itemID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil {
		refuse(w, http.StatusNotFound)
	}
	return id, err == nil
}

// playbackInfo answers a title's copies, as Jellyfin's PlaybackInfoResponse, the copy the app
// asked for or photon would play first, and the play session the app reports on. Where the app
// sends its device profile, the first is decided for it: played as it is where it plays that,
// else made into HLS it takes, at a TranscodingUrl. Nothing is started until the app plays.
func (a *API) playbackInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req playbackRequest
	if r.Method == http.MethodPost && !readJSON(w, r, &req) {
		return
	}
	req.MediaSourceID = cmp.Or(req.MediaSourceID, query(r, "mediaSourceId"))
	version, _ := uuid.Parse(req.MediaSourceID)
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, version)
	if errors.Is(err, store.ErrNotFound) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	versions, err := a.svc.Catalogue.Versions(r.Context(), []uuid.UUID{id})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	session := uuid.NewV7()
	out := struct {
		MediaSources  []mediaSource `json:"MediaSources"`
		PlaySessionID string        `json:"PlaySessionId"`
	}{MediaSources: []mediaSource{}, PlaySessionID: guid(session)}
	for _, v := range versions[id] {
		src := sourceOf(v)
		if v.ID == c.Version {
			out.MediaSources = slices.Insert(out.MediaSources, 0, src)
		} else {
			out.MediaSources = append(out.MediaSources, src)
		}
	}
	if req.DeviceProfile != nil && len(out.MediaSources) > 0 {
		remote, err := playback.RemoteLimit(r.Context(), a.svc.Network, a.svc.Proxies.Client(r))
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.decide(r, &out.MediaSources[0], id, session, c, req, remote)
	}
	writeJSON(w, out)
}

// playbackRequest is Jellyfin's PlaybackInfoDto, as much of it as photon decides by.
type playbackRequest struct {
	MediaSourceID       string         `json:"MediaSourceId"`
	AudioStreamIndex    *int           `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int           `json:"SubtitleStreamIndex"`
	StartTimeTicks      int64          `json:"StartTimeTicks"`
	MaxStreamingBitrate int64          `json:"MaxStreamingBitrate"`
	DeviceProfile       *deviceProfile `json:"DeviceProfile"`
}

// decide says how the app plays a copy: as it is, where its profile plays that; else as HLS photon
// makes of it, copying what the app plays and encoding the rest, where the app takes HLS photon
// makes; else neither, and the app plays what it can of the file. The app's bitrate is kept within
// remote, the server's limit for it, in kbps.
func (a *API) decide(r *http.Request, src *mediaSource, item, session uuid.UUID, c store.PlayCopy, req playbackRequest, remote int) {
	p := *req.DeviceProfile
	limit := int64(playback.Capped(int(cmp.Or(req.MaxStreamingBitrate, p.MaxStreamingBitrate)/1000), remote)) * 1000
	tracks := direct(c, req.AudioStreamIndex)
	sub, picked := subtitleOf(c, req.SubtitleStreamIndex)
	if p.playsDirectly(c, streamAt(c, tracks.Video), streamAt(c, tracks.Audio), picked, limit) {
		return
	}
	src.SupportsDirectPlay, src.SupportsDirectStream = false, false
	t, segments, ok := p.hls(streamAt(c, tracks.Audio))
	if !ok {
		return
	}
	var audio *int
	if tracks.Audio != nil {
		audio = &tracks.Audio.Stream
	}
	chosen := domain.ChosenTracks{Audio: audio, Subtitle: sub}
	if picked != nil && picked.external {
		chosen.SubtitleFile = &picked.file
	}
	d, err := playback.Decide(p.hlsProfile(t, segments, c, limit), playback.CopyOf(c), chosen, a.svc.Encoding)
	if err != nil || d.Video == nil {
		return
	}
	src.SupportsTranscoding, src.TranscodingSubProtocol, src.TranscodingContainer = true, "hls", transcodingContainers[segments]
	src.TranscodingURL = a.transcodingURL(r, item, session, transcode{
		Version: c.Version, Method: d.Method, Video: *d.Video, Audio: d.Audio, Subtitle: sub, Reasons: d.Reasons,
		Segments: segments, StartMS: req.StartTimeTicks / ticksPerMS,
	})
}

// transcodingContainers are Jellyfin's names for the segments of each format.
var transcodingContainers = map[domain.SegmentFormat]string{domain.SegmentsFMP4: "mp4", domain.SegmentsMPEGTS: "ts"}

// direct is a copy's tracks played as they are: its first video, and the audio an app chose, else
// its default, else its first.
func direct(c store.PlayCopy, audio *int) playback.Decision {
	d := playback.Decision{Method: domain.PlayDirect}
	var first, marked *domain.Stream
	for i, s := range c.Streams {
		switch s.Kind {
		case domain.StreamVideo:
			if d.Video == nil {
				d.Video = &domain.VideoPlan{Stream: s.Index, Codec: s.Codec}
			}
		case domain.StreamAudio:
			if audio != nil && *audio == s.Index {
				d.Audio = &domain.AudioPlan{Stream: s.Index}
			}
			if first == nil {
				first = &c.Streams[i]
			}
			if s.Default && marked == nil {
				marked = &c.Streams[i]
			}
		case domain.StreamSubtitle:
		}
	}
	if d.Audio == nil {
		if chosen := cmp.Or(marked, first); chosen != nil {
			d.Audio = &domain.AudioPlan{Stream: chosen.Index}
		}
	}
	return d
}

// streamAt is the copy's track a plan names, nil for none.
func streamAt[P interface {
	*domain.VideoPlan | *domain.AudioPlan
}](c store.PlayCopy, plan P) *domain.Stream {
	var index int
	switch p := any(plan).(type) {
	case *domain.VideoPlan:
		if p == nil {
			return nil
		}
		index = p.Stream
	case *domain.AudioPlan:
		if p == nil {
			return nil
		}
		index = p.Stream
	}
	for i := range c.Streams {
		if c.Streams[i].Index == index {
			return &c.Streams[i]
		}
	}
	return nil
}

// subtitleOf is the subtitle an app chose by its index: one of the copy's own tracks, whose index
// photon's decision takes, or a file beside it, numbered after them.
func subtitleOf(c store.PlayCopy, index *int) (*int, *subtitleChoice) {
	if index == nil || *index < 0 {
		return nil, nil
	}
	base := 0
	for _, s := range c.Streams {
		if s.Kind == domain.StreamSubtitle && s.Index == *index {
			return index, &subtitleChoice{codec: s.Codec}
		}
		base = max(base, s.Index+1)
	}
	if n := *index - base; n >= 0 && n < len(c.Subtitles) {
		return nil, &subtitleChoice{codec: c.Subtitles[n].Codec, external: true, file: c.Subtitles[n].ID}
	}
	return nil, nil
}

// errSeveralFiles is a copy in several files asked for as one: its files are joined only in HLS,
// which every app taking HLS is given at its TranscodingUrl.
var errSeveralFiles = errors.New("a copy in several files plays only as HLS")

// stream serves a copy's file as it is, the copy an app names or the one photon would play, with
// ranges, so an app seeks by asking for the bytes it wants. A copy in several files is refused:
// see errSeveralFiles.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	version, _ := uuid.Parse(query(r, "mediaSourceId"))
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, version)
	switch {
	case errors.Is(err, store.ErrNotFound) || err == nil && len(c.Parts) == 0:
		refuse(w, http.StatusNotFound)
		return
	case err != nil:
		a.internal(w, r, err)
		return
	case len(c.Parts) > 1:
		a.logger.InfoContext(r.Context(), "jellyfin stream refused", slog.Any("err", errSeveralFiles))
		refuse(w, http.StatusConflict)
		return
	}
	a.serveFile(w, r, func(ctx context.Context) (string, string, error) { return a.svc.Playing.PartFile(ctx, c.Parts[0].ID) })
}

func (a *API) serveFile(w http.ResponseWriter, r *http.Request, where func(context.Context) (root, rel string, err error)) {
	root, rel, err := where(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	f, err := library.Open(root, rel)
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
	// The bare file keeps the copy in the kernel: sendfile takes only an *os.File.
	http.ServeContent(w, r, rel, info.ModTime(), f)
}

// subtitle serves a subtitle file beside a copy, by the index its media source gives it.
func (a *API) subtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	source, err := uuid.Parse(r.PathValue("sourceId"))
	index, ierr := strconv.Atoi(r.PathValue("index"))
	if err != nil || ierr != nil {
		refuse(w, http.StatusNotFound)
		return
	}
	if _, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, source); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			refuse(w, http.StatusNotFound)
			return
		}
		a.internal(w, r, err)
		return
	}
	versions, err := a.svc.Catalogue.Versions(r.Context(), []uuid.UUID{id})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	n := slices.IndexFunc(versions[id], func(v store.VersionPage) bool { return v.ID == source })
	if n < 0 {
		refuse(w, http.StatusNotFound)
		return
	}
	v := versions[id][n]
	file := index - externalBase(v)
	if file < 0 || file >= len(v.Subtitles) {
		refuse(w, http.StatusNotFound)
		return
	}
	a.serveFile(w, r, func(ctx context.Context) (string, string, error) {
		return a.svc.Playing.SubtitleFile(ctx, v.Subtitles[file].ID)
	})
}

// report is Jellyfin's PlaybackStartInfo, PlaybackProgressInfo and PlaybackStopInfo, as much of
// them as photon keeps.
type report struct {
	ItemID              string `json:"ItemId"`
	MediaSourceID       string `json:"MediaSourceId"`
	PlaySessionID       string `json:"PlaySessionId"`
	PositionTicks       int64  `json:"PositionTicks"`
	IsPaused            bool   `json:"IsPaused"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
}

// playID is the playback a play session names: the id PlaybackInfo gave it, or one made of an id
// of the app's own, so every app's playback is one the dashboard shows.
func playID(session string) uuid.UUID {
	if id, err := uuid.Parse(session); err == nil {
		return id
	}
	h := fnv.New128a()
	h.Write([]byte(session))
	var id uuid.UUID
	copy(id[:], h.Sum(nil))
	return id
}

// reported records where an app says its playback is. A playback the app plays as it is is
// started by its first report, as the dashboard shows it from then; one in HLS was started when
// its playlist was fetched. A stop of one never started keeps where the profile got to in the
// title.
func (a *API) reported(stopped bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rep report
		if !readJSON(w, r, &rep) {
			return
		}
		profile := sessionOf(r).Profile.ID
		position := time.Duration(rep.PositionTicks/ticksPerMS) * time.Millisecond
		state := domain.StatePlaying
		if rep.IsPaused {
			state = domain.StatePaused
		}
		tracks := domain.ChosenTracks{Audio: rep.AudioStreamIndex}
		if rep.SubtitleStreamIndex != nil && *rep.SubtitleStreamIndex >= 0 {
			tracks.Subtitle = rep.SubtitleStreamIndex
		}
		id := playID(cmp.Or(rep.PlaySessionID, guid(sessionOf(r).ID)+rep.ItemID))
		progress := func(ctx context.Context) error {
			_, err := a.svc.Playbacks.Progress(ctx, profile, id, position, state, tracks)
			return err
		}
		var err error
		switch {
		case stopped:
			_, err = a.svc.Playbacks.Stop(r.Context(), profile, id, position)
			if errors.Is(err, playback.ErrNoPlayback) {
				err = a.saveProgress(r.Context(), profile, rep.ItemID, position)
			}
		default:
			err = progress(r.Context())
			if errors.Is(err, playback.ErrNoPlayback) {
				// A report told to another node at once may have started it first.
				if err = a.startDirect(r, id, rep); err == nil || errors.Is(err, playback.ErrStarted) {
					err = progress(r.Context())
				}
			}
		}
		if isNotFound(err) {
			err = nil
		}
		if err != nil {
			a.internal(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// startDirect starts the playback the dashboard shows of a copy an app plays as it is.
func (a *API) startDirect(r *http.Request, id uuid.UUID, rep report) error {
	item, ok := parseID(rep.ItemID)
	if !ok {
		return store.ErrNotFound
	}
	version, _ := uuid.Parse(rep.MediaSourceID)
	s := sessionOf(r)
	c, err := a.svc.Playing.Playable(r.Context(), s.Profile.ID, item, version)
	if err != nil {
		return err
	}
	title, err := a.svc.Playing.PlaybackTitle(r.Context(), item)
	if err != nil {
		return err
	}
	sub, _ := subtitleOf(c, rep.SubtitleStreamIndex)
	_, err = a.svc.Playbacks.Start(r.Context(), id, domain.PlayDirect, playback.Card(s, a.svc.Proxies.Client(r).String(), title, c, direct(c, rep.AudioStreamIndex), domain.ChosenTracks{Subtitle: sub}))
	return err
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

func parseID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil
}

// saveProgress keeps where a profile got to in a title it played without a playback of photon's.
// A report that names no title of photon's keeps nothing.
func (a *API) saveProgress(ctx context.Context, profile uuid.UUID, item string, position time.Duration) error {
	id, ok := parseID(item)
	if !ok || position <= 0 {
		return nil
	}
	length, err := a.svc.Watching.Length(ctx, id)
	if err != nil || length <= 0 {
		return err
	}
	_, err = a.svc.Watching.SaveProgress(ctx, profile, id, position, length, domain.ReachStart, nil)
	if isNotFound(err) || errors.Is(err, store.ErrSuperseded) {
		return nil
	}
	return err
}

// mark sets or clears what a profile has made of a title, and answers its UserData as it is then.
func (a *API) mark(set func(ctx context.Context, profile, item uuid.UUID) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := itemID(w, r)
		if !ok {
			return
		}
		profile := sessionOf(r).Profile.ID
		err := set(r.Context(), profile, id)
		if isNotFound(err) {
			refuse(w, http.StatusNotFound)
			return
		}
		if err != nil {
			a.internal(w, r, err)
			return
		}
		p, err := a.svc.Catalogue.Title(r.Context(), profile, id)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		writeJSON(w, a.userData(id, p.State, 0, p.Kind))
	}
}

// segmentTypes are Jellyfin's MediaSegmentType for each kind of marker.
var segmentTypes = map[domain.MarkerKind]string{
	domain.MarkerIntro: "Intro", domain.MarkerCredits: "Outro", domain.MarkerRecap: "Recap", domain.MarkerPreview: "Preview",
}

type segment struct {
	ID         string `json:"Id"`
	ItemID     string `json:"ItemId"`
	Type       string `json:"Type"`
	StartTicks int64  `json:"StartTicks"`
	EndTicks   int64  `json:"EndTicks"`
}

// mediaSegments answers a title's intro, credits, recap and preview, as Jellyfin's media segments: the
// markers of the copy photon would play, which apps offer to skip.
func (a *API) mediaSegments(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, uuid.UUID{})
	if isNotFound(err) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	versions, err := a.svc.Catalogue.Versions(r.Context(), []uuid.UUID{id})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	wanted := values(r, "includeSegmentTypes")
	out := struct {
		Items            []segment `json:"Items"`
		TotalRecordCount int       `json:"TotalRecordCount"`
		StartIndex       int       `json:"StartIndex"`
	}{Items: []segment{}}
	for _, v := range versions[id] {
		if v.ID != c.Version {
			continue
		}
		for _, m := range v.Markers {
			kind := segmentTypes[m.Kind]
			if len(wanted) > 0 && !has(wanted, kind) {
				continue
			}
			h := fnv.New128a()
			h.Write(v.ID[:])
			h.Write([]byte(m.Kind))
			out.Items = append(out.Items, segment{
				ID: hex.EncodeToString(h.Sum(nil)), ItemID: guid(id), Type: kind,
				StartTicks: m.StartMS * ticksPerMS, EndTicks: m.EndMS * ticksPerMS,
			})
		}
	}
	out.TotalRecordCount = len(out.Items)
	writeJSON(w, out)
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
