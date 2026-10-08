package jellyfin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type previews interface {
	Trickplay(ctx context.Context, profile, part uuid.UUID) (store.Trickplay, error)
}

type previewFiles interface {
	Sheet(ctx context.Context, part uuid.UUID, n int) (blob.Object, error)
}

// trickplayInfo is Jellyfin's TrickplayInfoDto. Its Bandwidth, the most a player fetching the
// sheets takes, is not known: photon keeps no sheet's size, and no app reads it.
type trickplayInfo struct {
	Width          int `json:"Width"`
	Height         int `json:"Height"`
	TileWidth      int `json:"TileWidth"`
	TileHeight     int `json:"TileHeight"`
	ThumbnailCount int `json:"ThumbnailCount"`
	Interval       int `json:"Interval"`
	Bandwidth      int `json:"Bandwidth"`
}

// trickplay fills the thumbnail sheets of each copy in one file, by the copy's id and their width.
// A copy in several files has none here: photon times each file's thumbnails from that file's
// start, and a Jellyfin copy has one run of sheets from its own.
func (it *item) trickplay(versions []store.VersionPage) {
	for _, v := range versions {
		if v.Parts != 1 || len(v.Trickplay) != 1 {
			continue
		}
		t := v.Trickplay[0]
		if it.Trickplay == nil {
			it.Trickplay = map[string]map[int]trickplayInfo{}
		}
		it.Trickplay[guid(v.ID)] = map[int]trickplayInfo{t.Width: {
			Width: t.Width, Height: t.Height, TileWidth: t.Columns, TileHeight: t.Rows, ThumbnailCount: t.Thumbnails, Interval: t.IntervalMS,
		}}
	}
}

// trickplayFile serves a copy's thumbnail sheets at the width the copy's Trickplay gives: their
// playlist, tiles.m3u8, or a sheet by its number, 0.jpg on. The copy is the one the app names, or
// the one photon would play.
func (a *API) trickplayFile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	width, err := strconv.Atoi(r.PathValue("width"))
	version, ok := optionalID(query(r, "mediaSourceId"))
	if err != nil || !ok {
		a.refuse(w, http.StatusNotFound)
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	c, err := a.svc.Playing.Playable(r.Context(), profile, id, version)
	if err == nil && len(c.Parts) != 1 {
		err = store.ErrNotFound
	}
	var t store.Trickplay
	if err == nil {
		t, err = a.svc.Previews.Trickplay(r.Context(), profile, c.Parts[0].ID)
	}
	if isNotFound(err) || err == nil && t.Width != width {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	name := r.PathValue("file")
	if strings.EqualFold(name, "tiles.m3u8") {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		a.write(w, []byte(tiles(t, url.Values{"MediaSourceId": {guid(c.Version)}, "ApiKey": {appOf(r).Token}}.Encode())))
		return
	}
	number, isSheet := strings.CutSuffix(name, ".jpg")
	n, err := strconv.Atoi(number)
	if !isSheet || err != nil || n < 0 || n >= t.Sheets {
		a.refuse(w, http.StatusNotFound)
		return
	}
	o, err := a.svc.PreviewFiles.Sheet(r.Context(), c.Parts[0].ID, n)
	if errors.Is(err, os.ErrNotExist) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// A part's sheets are made again only from the same bytes, so an app may keep one for good.
	err = blob.Serve(w, r, o, "", http.Header{"Content-Type": {"image/jpeg"}, "Cache-Control": {"private, max-age=31536000, immutable"}})
	if err != nil {
		a.internal(w, r, err)
	}
}

// tiles is the playlist of a part's sheets as Jellyfin writes one, each sheet's address carrying
// query, as no app sends its token with what a playlist names. Jellyfin's target duration is its
// count of sheets; this one is a full sheet's seconds, as HLS means it.
func tiles(t store.Trickplay, query string) string {
	per := t.Columns * t.Rows
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:" + strconv.Itoa((per*t.IntervalMS+999)/1000) +
		"\n#EXT-X-VERSION:7\n#EXT-X-MEDIA-SEQUENCE:1\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-IMAGES-ONLY\n")
	for n := range t.Sheets {
		b.WriteString("#EXTINF:" + seconds(min(per, t.Thumbnails-n*per)*t.IntervalMS) + ",\n" +
			"#EXT-X-TILES:RESOLUTION=" + strconv.Itoa(t.Width) + "x" + strconv.Itoa(t.Height) +
			",LAYOUT=" + strconv.Itoa(t.Columns) + "x" + strconv.Itoa(t.Rows) + ",DURATION=" + seconds(t.IntervalMS) + "\n" +
			strconv.Itoa(n) + ".jpg?" + query + "\n")
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func seconds(ms int) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
}
