package httpapi

import (
	"context"
	"encoding/csv"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type history interface {
	History(ctx context.Context, profile uuid.UUID, before store.Before, limit int) ([]store.Play, bool, error)
	PlayRecords(ctx context.Context, profile uuid.UUID, before store.Before, limit int) ([]store.PlayRecord, bool, error)
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

// historyFileBatch is how many plays a file of the history reads at a time: the file is written
// as it is read, and no read is held open while the client takes what was written.
const historyFileBatch = 500

// historyFileColumns are a file of the history's header. Each row is one play; an episode's ids
// are its show's, its season and episode saying which of it, as Trakt's import finds one.
var historyFileColumns = []string{
	"started_at", "stopped_at", "profile", "kind", "title", "year", "show_title", "season", "episode",
	"position_ms", "method", "tmdb_id", "tvdb_id", "imdb_id",
}

func (a *API) ownHistoryFile(w http.ResponseWriter, r *http.Request) {
	a.historyFile(w, r, auth.SessionOf(r.Context()).Profile.ID)
}

func (a *API) adminHistoryFile(w http.ResponseWriter, r *http.Request) {
	profile, ok := a.queryID(w, r, "profile")
	if !ok {
		return
	}
	a.historyFile(w, r, profile)
}

// historyFile writes plays as CSV, the latest first, a batch at a time. The first batch is read
// before anything is written, so a failure then is an error; one later can only cut the file
// short, which the client sees as a download that did not finish.
func (a *API) historyFile(w http.ResponseWriter, r *http.Request, profile uuid.UUID) {
	plays, more, err := a.svc.History.PlayRecords(r.Context(), profile, store.Newest, historyFileBatch)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	name := "photon-history-" + time.Now().UTC().Format("20060102") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	out := csv.NewWriter(w)
	rows := [][]string{historyFileColumns}
	for {
		for _, p := range plays {
			rows = append(rows, []string{
				p.StartedAt.UTC().Format(time.RFC3339), p.StoppedAt.UTC().Format(time.RFC3339), p.ProfileName,
				string(p.Kind), p.Title, csvNumber(p.Year), p.ShowTitle, csvNumber(p.SeasonNumber), csvNumber(p.EpisodeNumber),
				strconv.FormatInt(p.PositionMS, 10), string(p.Method), p.TMDB, p.TVDB, p.IMDb,
			})
		}
		// A write fails only for a client gone, whom nothing more reaches.
		if out.WriteAll(rows) != nil || !more {
			return
		}
		rows = rows[:0]
		last := plays[len(plays)-1]
		plays, more, err = a.svc.History.PlayRecords(r.Context(), profile, store.Before{At: last.StoppedAt, ID: last.ID}, historyFileBatch)
		if err != nil {
			a.logger.ErrorContext(r.Context(), "history file cut short", slog.Any("err", err))
			panic(http.ErrAbortHandler)
		}
	}
}

func csvNumber(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
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
		{
			pattern: "GET /api/v1/history/file", access: signedIn, summary: "Download the profile's plays as CSV, the latest first",
			status: http.StatusOK, reply: asFile{"text/csv"}, handle: a.ownHistoryFile,
		},
		{
			pattern: "GET /api/v1/admin/history/file", access: admin, summary: "Download everyone's plays, or one profile's, as CSV",
			query:  []param{{"profile", uuid.UUID{}, "Only this profile's plays."}},
			status: http.StatusOK, reply: asFile{"text/csv"}, handle: a.adminHistoryFile,
		},
	}
}
