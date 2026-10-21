package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type history interface {
	History(ctx context.Context, profile uuid.UUID, before store.Before, limit int) ([]store.Play, bool, error)
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
	a.history(w, r, auth.SessionOf(r.Context()).Profile.ID)
}

// adminHistory answers everyone's plays, or one profile's, the latest first, as Plex's dashboard
// lists them.
func (a *API) adminHistory(w http.ResponseWriter, r *http.Request) {
	profile, ok := a.queryID(w, r, "profile")
	if !ok {
		return
	}
	a.history(w, r, profile)
}

func (a *API) history(w http.ResponseWriter, r *http.Request, profile uuid.UUID) {
	before, limit, ok := a.cursorPaging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	plays, more, err := a.svc.History.History(r.Context(), profile, before, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]historyEntryJSON, len(plays))
	for i, p := range plays {
		out[i] = historyEntryJSON{
			ID: p.ID, ProfileID: p.Profile, Title: cardOf(p.Card), Method: p.Method,
			StartedAt: p.StartedAt.UTC(), StoppedAt: p.StoppedAt.UTC(), PositionMS: p.PositionMS,
		}
	}
	page := cursorPageJSON[historyEntryJSON]{Items: out}
	if more {
		last := plays[len(plays)-1]
		page.Next = nextOf(last.StoppedAt, last.ID)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, page)
}

func (a *API) historyRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/history", access: signedIn, summary: "Page the profile's plays, the latest first",
			query: cursorParams, status: http.StatusOK, reply: cursorPageJSON[historyEntryJSON]{}, handle: a.ownHistory,
		},
		{
			pattern: "GET /api/v1/admin/history", access: admin, summary: "Page everyone's plays, or one profile's",
			query:  append([]param{{"profile", uuid.UUID{}, "Only this profile's plays."}}, cursorParams...),
			status: http.StatusOK, reply: cursorPageJSON[historyEntryJSON]{}, handle: a.adminHistory,
		},
	}
}
