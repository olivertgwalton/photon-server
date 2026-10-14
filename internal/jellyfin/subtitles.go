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
	versions, err := a.svc.Catalogue.Versions(r.Context(), []uuid.UUID{id})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	n := slices.IndexFunc(versions[id], func(v store.VersionPage) bool { return v.ID == source })
	if n < 0 {
		a.refuse(w, http.StatusNotFound)
		return
	}
	v := versions[id][n]
	file := index - externalBase(v)
	if file < 0 || file >= len(v.Subtitles) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	a.serveFile(w, r, func(ctx context.Context) (media.Input, error) {
		root, rel, err := a.svc.Playing.SubtitleFile(ctx, v.Subtitles[file].ID)
		if err != nil {
			return media.Input{}, err
		}
		return library.OpenMedia(root, rel)
	})
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

// readOutSubtitles marks each stream of a copy played as it is that the app draws read out beside
// it External, at the address Jellyfin gives it, carrying the app's token, as an app sends none
// with what it fetches for its player.
func readOutSubtitles(r *http.Request, src *mediaSource, item uuid.UUID, c store.PlayCopy, p deviceProfile) {
	for i, m := range src.MediaStreams {
		n := slices.IndexFunc(c.Streams, func(s domain.Stream) bool { return s.Kind == domain.StreamSubtitle && s.Index == m.Index })
		if m.IsExternal || n < 0 {
			continue
		}
		if format := p.readOut(c.Streams[n].Codec); format != "" {
			src.MediaStreams[i].DeliveryMethod = "External"
			src.MediaStreams[i].DeliveryURL = "/Videos/" + guid(item) + "/" + src.ID + "/Subtitles/" + strconv.Itoa(m.Index) + "/0/Stream." + format +
				"?" + url.Values{"ApiKey": {appOf(r).Token}}.Encode()
		}
	}
}
