package httpapi

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
)

type lyricsJSON struct {
	Lines []lyricLineJSON `json:"lines"`
}

type lyricLineJSON struct {
	// StartMS is where the line is sung, in lyrics timed line by line alone.
	StartMS *int64 `json:"start_ms,omitzero"`
	Text    string `json:"text"`
}

// lyrics answers the lyrics of the version of a track that plays: an LRC file beside it, else its
// tags', else what its library's lyrics sources find.
func (a *API) lyrics(r *http.Request, _ none) (lyricsJSON, error) {
	ctx := r.Context()
	id := pathUUID(r, "id")
	v, err := a.svc.Playing.Playable(ctx, auth.SessionOf(ctx).Profile.ID, id, uuid.UUID{})
	if err != nil {
		return lyricsJSON{}, err
	}
	l, err := a.svc.Lyrics.Find(ctx, id, v.Parts[0].ID)
	if errors.Is(err, library.ErrNoLyrics) {
		return lyricsJSON{}, refuse(codeNotFound, "it has no lyrics")
	}
	if err != nil {
		return lyricsJSON{}, err
	}
	return lyricsJSON{Lines: each(l.Lines, func(line media.LyricLine) lyricLineJSON {
		out := lyricLineJSON{Text: line.Text}
		switch l.Timing {
		case domain.TimedByLine:
			ms := line.Start.Milliseconds()
			out.StartMS = &ms
		case domain.Untimed:
		}
		return out
	})}, nil
}
