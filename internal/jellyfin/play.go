package jellyfin

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	Start(ctx context.Context, method domain.PlayMethod, card domain.PlaybackCard) (domain.Playback, error)
	Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState, tracks domain.ChosenTracks) (domain.Reach, error)
	Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error)
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

// playbackInfo answers a title's copies, as Jellyfin's PlaybackInfoResponse, and where the app is
// about to play starts the playback the dashboard shows: its id is the play session the app
// reports on. Every copy plays directly, its file as it is; an app plays what it can of it.
func (a *API) playbackInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req struct {
		MediaSourceID       string `json:"MediaSourceId"`
		AudioStreamIndex    *int   `json:"AudioStreamIndex"`
		SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
		IsPlayback          bool   `json:"IsPlayback"`
	}
	if r.Method == http.MethodPost && !readJSON(w, r, &req) {
		return
	}
	req.MediaSourceID = cmp.Or(req.MediaSourceID, query(r, "mediaSourceId"))
	version, _ := uuid.Parse(req.MediaSourceID)
	s := sessionOf(r)
	c, err := a.svc.Playing.Playable(r.Context(), s.Profile.ID, id, version)
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
	out := struct {
		MediaSources  []mediaSource `json:"MediaSources"`
		PlaySessionID string        `json:"PlaySessionId"`
	}{MediaSources: []mediaSource{}, PlaySessionID: guid(uuid.NewV7())}
	for _, v := range versions[id] {
		src := sourceOf(v)
		// The copy asked for, or the one photon would play, first: an app plays the first.
		if v.ID == c.Version {
			out.MediaSources = slices.Insert(out.MediaSources, 0, src)
		} else {
			out.MediaSources = append(out.MediaSources, src)
		}
	}
	if req.IsPlayback {
		title, err := a.svc.Playing.PlaybackTitle(r.Context(), id)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		p, err := a.svc.Playbacks.Start(r.Context(), domain.PlayDirect, playback.Card(s, a.svc.Proxies.Client(r).String(), title, c, direct(c, req.AudioStreamIndex), embedded(c, req.SubtitleStreamIndex)))
		if err != nil {
			a.internal(w, r, err)
			return
		}
		out.PlaySessionID = guid(p.ID)
	}
	writeJSON(w, out)
}

// direct is a copy played as it is, with the audio an app chose or the first.
func direct(c store.PlayCopy, audio *int) playback.Decision {
	d := playback.Decision{Method: domain.PlayDirect}
	for _, s := range c.Streams {
		switch {
		case s.Kind == domain.StreamVideo && d.Video == nil:
			d.Video = &domain.VideoPlan{Stream: s.Index, Codec: s.Codec}
		case s.Kind == domain.StreamAudio && (d.Audio == nil && audio == nil || audio != nil && *audio == s.Index):
			d.Audio = &domain.AudioPlan{Stream: s.Index}
		}
	}
	return d
}

// embedded is a subtitle an app chose, where it is one of the copy's own tracks.
func embedded(c store.PlayCopy, subtitle *int) *int {
	if subtitle != nil && slices.ContainsFunc(c.Streams, func(s domain.Stream) bool {
		return s.Kind == domain.StreamSubtitle && s.Index == *subtitle
	}) {
		return subtitle
	}
	return nil
}

// stream serves a copy's file as it is, the copy an app names or the one photon would play, with
// ranges, so an app seeks by asking for the bytes it wants. A copy in several parts is served by its
// first; ponytail: Jellyfin's apps play one file a copy, so parts after the first wait on HLS.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if name := r.PathValue("stream"); name != "" && !strings.HasPrefix(strings.ToLower(name), "stream.") {
		refuse(w, http.StatusNotFound)
		return
	}
	version, _ := uuid.Parse(query(r, "mediaSourceId"))
	c, err := a.svc.Playing.Playable(r.Context(), sessionOf(r).Profile.ID, id, version)
	if errors.Is(err, store.ErrNotFound) || err == nil && len(c.Parts) == 0 {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
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
	PlaySessionID       string `json:"PlaySessionId"`
	PositionTicks       int64  `json:"PositionTicks"`
	IsPaused            bool   `json:"IsPaused"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
}

// reported records where an app says its playback is: on the playback its PlaybackInfo started,
// or, for one photon did not start (an app that played without asking), on the title alone.
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
		id, perr := uuid.Parse(rep.PlaySessionID)
		var err error
		switch {
		case perr != nil:
			err = a.saveProgress(r.Context(), profile, rep.ItemID, position)
		case stopped:
			_, err = a.svc.Playbacks.Stop(r.Context(), profile, id, position)
		default:
			_, err = a.svc.Playbacks.Progress(r.Context(), profile, id, position, state, tracks)
		}
		if errors.Is(err, playback.ErrNoPlayback) {
			err = a.saveProgress(r.Context(), profile, rep.ItemID, position)
		}
		if err != nil {
			a.internal(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
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
