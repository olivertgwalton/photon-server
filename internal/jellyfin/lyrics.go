package jellyfin

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/library"
)

// lyricDto is Jellyfin's lyrics of a track: Start, in ticks, on each line of timed lyrics alone.
type lyricDto struct {
	Metadata struct {
		IsSynced bool `json:"IsSynced"`
	} `json:"Metadata"`
	Lyrics []lyricLine `json:"Lyrics"`
}

type lyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"`
}

// lyrics answers /Audio/{itemId}/Lyrics, which Finamp and Jellyfin's apps show beside a track.
func (a *API) lyrics(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	ctx := r.Context()
	c, err := a.svc.Playing.Playable(ctx, auth.SessionOf(ctx).Profile.ID, id, uuid.UUID{})
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	l, err := a.svc.Parts.Lyrics(ctx, c.Parts[0].ID)
	if errors.Is(err, library.ErrNoLyrics) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var out lyricDto
	out.Metadata.IsSynced = l.Synced
	for _, line := range l.Lines {
		row := lyricLine{Text: line.Text}
		if l.Synced {
			ticks := line.Start.Milliseconds() * ticksPerMS
			row.Start = &ticks
		}
		out.Lyrics = append(out.Lyrics, row)
	}
	a.writeJSON(w, out)
}
