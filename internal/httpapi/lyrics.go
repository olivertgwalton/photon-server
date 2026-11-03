package httpapi

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
)

type lyricsJSON struct {
	// Synced is lyrics each line of which says where it is sung.
	Synced bool            `json:"synced"`
	Lines  []lyricLineJSON `json:"lines"`
}

type lyricLineJSON struct {
	// StartMS is where the line is sung, in synced lyrics alone.
	StartMS *int64 `json:"start_ms,omitzero"`
	Text    string `json:"text"`
}

// lyrics answers the lyrics of the version of a track that plays: an LRC file beside it, else its
// tags'.
func (a *API) lyrics(r *http.Request, _ none) (lyricsJSON, error) {
	ctx := r.Context()
	v, err := a.svc.Playing.Playable(ctx, auth.SessionOf(ctx).Profile.ID, pathUUID(r, "id"), uuid.UUID{})
	if err != nil {
		return lyricsJSON{}, err
	}
	l, err := a.svc.Parts.Lyrics(ctx, v.Parts[0].ID)
	if errors.Is(err, library.ErrNoLyrics) {
		return lyricsJSON{}, refuse(codeNotFound, "it has no lyrics")
	}
	if err != nil {
		return lyricsJSON{}, err
	}
	return lyricsJSON{Synced: l.Synced, Lines: each(l.Lines, func(line media.LyricLine) lyricLineJSON {
		out := lyricLineJSON{Text: line.Text}
		if l.Synced {
			ms := line.Start.Milliseconds()
			out.StartMS = &ms
		}
		return out
	})}, nil
}
