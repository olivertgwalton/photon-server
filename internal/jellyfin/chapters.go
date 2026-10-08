package jellyfin

import (
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// chapterInfo is Jellyfin's ChapterInfo. ImageDateModified is when a chapter's picture was made,
// which photon does not keep: it is written as Jellyfin writes it of a chapter with none, and an
// app keys a picture on its tag.
type chapterInfo struct {
	StartPositionTicks int64     `json:"StartPositionTicks"`
	Name               string    `json:"Name,omitempty"`
	ImageTag           string    `json:"ImageTag,omitempty"`
	ImageDateModified  time.Time `json:"ImageDateModified"`
}

// chapters fills a title's chapters: those of its first copy, as a Jellyfin item's are its own
// file's.
func (it *item) chapters(versions []store.VersionPage) {
	if len(versions) == 0 {
		return
	}
	for _, c := range versions[0].Chapters {
		ch := chapterInfo{StartPositionTicks: c.StartMS * ticksPerMS, Name: c.Title}
		if c.Image != "" {
			ch.ImageTag = chapterTag(c.Part, c.Idx)
		}
		it.Chapters = append(it.Chapters, ch)
	}
}

// chapterTag names a chapter's picture by its part and its place there. A part's pictures are made
// again only from the same bytes, so the tag stays good for as long as the part does.
func chapterTag(part uuid.UUID, idx int) string {
	return guid(part) + "-" + strconv.Itoa(idx)
}
