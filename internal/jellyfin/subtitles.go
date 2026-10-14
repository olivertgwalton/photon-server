package jellyfin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// subtitle serves a subtitle of a copy by the index its media source gives it: a stream of its
// file, read out as it is, or a file beside it.
func (a *API) subtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	source, err := uuid.Parse(r.PathValue("sourceId"))
	index, ierr := strconv.Atoi(r.PathValue("index"))
	if err != nil || ierr != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	c, err := a.svc.Playing.Playable(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, source)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.refuse(w, http.StatusNotFound)
			return
		}
		a.internal(w, r, err)
		return
	}
	if n := slices.IndexFunc(c.Streams, func(s domain.Stream) bool { return s.Kind == domain.StreamSubtitle && s.Index == index }); n >= 0 {
		a.readOut(w, r, c, c.Streams[n])
		return
	}
	_, chosen := subtitleOf(c, &index)
	if chosen == nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	n := slices.IndexFunc(c.Subtitles, func(f store.PlaySubtitle) bool { return f.ID == chosen.file })
	a.subtitleFile(w, r, c.Subtitles[n])
}

// trackEvent is a cue as Jellyfin's web app reads a text subtitle's, from its JSON (Stream.js).
type trackEvent struct {
	ID                 string `json:"Id"`
	Text               string `json:"Text"`
	StartPositionTicks int64  `json:"StartPositionTicks"`
	EndPositionTicks   int64  `json:"EndPositionTicks"`
}

// subtitleFile serves a subtitle file beside a copy in the format the file name asks for, as
// Jellyfin converts one: plain text as WebVTT, read in its charset as photon's player has it, or as
// the JSON of its cues Jellyfin's web app reads every text track from; VobSub, its .idx and .sub, in
// one Matroska file, as the web app's libbitsub reads it; else the file as it is, asked for in its
// own format. Any other format is refused: photon converts to no other. As for a stream, a start in
// the path is not applied.
func (a *API) subtitleFile(w http.ResponseWriter, r *http.Request, f store.PlaySubtitle) {
	ctx := r.Context()
	open := func() (media.Input, error) {
		root, rel, err := a.svc.Playing.SubtitleFile(ctx, f.ID)
		if err != nil {
			return media.Input{}, err
		}
		return library.OpenMedia(root, rel)
	}
	failed := func(err error) bool {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.refuse(w, http.StatusNotFound)
		case err != nil:
			a.internal(w, r, err)
		}
		return err != nil
	}
	language := string(domain.TagOf(f.Language))
	format := strings.ToLower(strings.TrimPrefix(path.Ext(r.PathValue("file")), "."))
	text := hls.TextSubtitle(f.Codec)
	switch {
	case f.Codec == "dvd_subtitle" && (format == "mks" || slices.Contains(subtitleFormats(f.Codec), format)):
		mks, err := a.svc.HLS.VobSub(ctx, open)
		if failed(err) {
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		a.write(w, mks)
	case text && slices.Contains(subtitleFormats("webvtt"), format):
		vtt, err := a.svc.HLS.WebVTT(ctx, open, language)
		if failed(err) {
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		a.write(w, []byte(vtt))
	case text && format == "js":
		cues, err := a.svc.HLS.Cues(ctx, open, language)
		if failed(err) {
			return
		}
		events := make([]trackEvent, len(cues))
		for i, c := range cues {
			events[i] = trackEvent{ID: strconv.Itoa(i + 1), Text: c.Text, StartPositionTicks: int64(c.Start / 100), EndPositionTicks: int64(c.End / 100)}
		}
		a.writeJSON(w, struct{ TrackEvents []trackEvent }{events})
	case f.Codec != "dvd_subtitle" && (slices.Contains(subtitleFormats(f.Codec), format) || hls.StyledSubtitle(f.Codec) && hls.StyledSubtitle(format)):
		a.serveFile(w, r, func(context.Context) (media.Input, error) { return open() })
	default:
		a.refuse(w, http.StatusNotFound)
	}
}

// readOut serves a styled or picture subtitle stream of a copy's file read out as it is, as the
// web player's is, where the file name asks for it in that format. A start in the path is not
// applied: Jellyfin serves a stream it need not convert as it is, from its start.
func (a *API) readOut(w http.ResponseWriter, r *http.Request, c store.PlayCopy, s domain.Stream) {
	name := hls.StyledName(s.Index)
	if hls.PictureSubtitle(s.Codec) {
		name = hls.PictureName(s.Index, s.Codec)
	}
	if !hls.ReadOut(s.Codec) || !readsOutAs(s.Codec, name, strings.ToLower(strings.TrimPrefix(path.Ext(r.PathValue("file")), "."))) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if len(c.Parts) != 1 {
		a.logger.InfoContext(r.Context(), "jellyfin subtitle refused", slog.Any("err", errSeveralFiles))
		a.refuse(w, http.StatusConflict)
		return
	}
	part := c.Parts[0].ID
	opening := context.WithoutCancel(r.Context())
	src := hls.SubtitleSource{Open: func() (media.Input, error) { return a.svc.Parts.Open(opening, part) }, Part: part, Streams: c.Streams}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, name)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.serveFile(w, r, func(context.Context) (media.Input, error) { return library.OpenMedia(dir, name) })
}

// readsOutAs is whether a stream of a codec, read out into a file of a name, is in a format an app
// asks for: its codec as Jellyfin's apps name it, or the file's own, and either of ASS and SSA for
// styled text, which is read out as ASS, SSA's superset.
func readsOutAs(codec, name, format string) bool {
	if hls.StyledSubtitle(codec) {
		return hls.StyledSubtitle(format)
	}
	return format == strings.TrimPrefix(path.Ext(name), ".") || slices.Contains(subtitleFormats(codec), format)
}

// externalSubtitles marks each subtitle of a copy played as it is that the app draws beside it
// External, at the address Jellyfin gives it, carrying the app's token, as an app sends none with
// what it fetches for its player: a stream read out of the file, or a file beside it, in the format
// the app takes it in.
func externalSubtitles(r *http.Request, src *mediaSource, item uuid.UUID, c store.PlayCopy, p deviceProfile) {
	for i, m := range src.MediaStreams {
		_, s := subtitleOf(c, &m.Index)
		if s == nil {
			continue
		}
		format := p.readOut(s.codec)
		if s.delivery == domain.SubtitleSidecar {
			format = p.sidecarFormat(s.codec)
		}
		if format != "" {
			src.MediaStreams[i].DeliveryMethod = "External"
			src.MediaStreams[i].DeliveryURL = "/Videos/" + guid(item) + "/" + src.ID + "/Subtitles/" + strconv.Itoa(m.Index) + "/0/Stream." + format +
				"?" + url.Values{"ApiKey": {appOf(r).Token}}.Encode()
		}
	}
}
