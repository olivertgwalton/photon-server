package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type history interface {
	History(ctx context.Context, profile uuid.UUID, offset, limit int) ([]store.Play, int64, error)
}

type historyEntryJSON struct {
	ID         uuid.UUID         `json:"id"`
	ProfileID  uuid.UUID         `json:"profile_id"`
	Title      cardJSON          `json:"title"`
	Method     domain.PlayMethod `json:"method"`
	StartedAt  time.Time         `json:"started_at"`
	StoppedAt  time.Time         `json:"stopped_at"`
	PositionMS int64             `json:"position_ms"`
}

// ownHistory answers the profile's plays, the latest first.
func (a *API) ownHistory(w http.ResponseWriter, r *http.Request) {
	a.history(w, r, sessionOf(r).Profile.ID)
}

// adminHistory answers everyone's plays, or one profile's, the latest first, as Plex's dashboard
// lists them.
func (a *API) adminHistory(w http.ResponseWriter, r *http.Request) {
	var profile uuid.UUID
	if s := r.URL.Query().Get("profile"); s != "" {
		var err error
		if profile, err = uuid.Parse(s); err != nil {
			writeProblem(w, a.logger, codeInvalidParameter, "profile is a profile's id")
			return
		}
	}
	a.history(w, r, profile)
}

func (a *API) history(w http.ResponseWriter, r *http.Request, profile uuid.UUID) {
	offset, limit, ok := a.paging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	plays, total, err := a.svc.History.History(r.Context(), profile, offset, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]historyEntryJSON, len(plays))
	for i, p := range plays {
		out[i] = historyEntryJSON{
			ID: p.ID, ProfileID: p.Profile, Title: cardsJSON([]store.Card{p.Card})[0], Method: p.Method,
			StartedAt: p.StartedAt.UTC(), StoppedAt: p.StoppedAt.UTC(), PositionMS: p.PositionMS,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[historyEntryJSON]{out, offset, total})
}
