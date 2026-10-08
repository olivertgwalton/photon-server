package jellyfin

import (
	"github.com/olivertgwalton/photon-server/internal/store"
)

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
