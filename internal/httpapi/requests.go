package httpapi

import (
	"context"
	"net/http"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type requests interface {
	AskFor(ctx context.Context, profile, discovery uuid.UUID, seasons []int) (store.Request, error)
	Requests(ctx context.Context, profile uuid.UUID, offset, limit int) ([]store.Request, int64, error)
	RequestsKept(ctx context.Context, by *uuid.UUID, state domain.RequestState, offset, limit int) ([]store.Request, int64, error)
	WithdrawRequest(ctx context.Context, profile, id uuid.UUID) error
	DecideRequest(ctx context.Context, id uuid.UUID, by *uuid.UUID, decider uuid.UUID, state domain.RequestState, reason string) (store.Request, error)
}

// requestablePages draws a title a profile may ask for as a title's page.
type requestablePages interface {
	Page(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
}

// maxRequestsLimit is as many requests as a page holds.
const maxRequestsLimit = 100

type requestJSON struct {
	ID        uuid.UUID       `json:"id"`
	ProfileID uuid.UUID       `json:"profile_id"`
	Profile   string          `json:"profile"`
	LibraryID uuid.UUID       `json:"library_id"`
	Kind      domain.ItemKind `json:"kind"`
	Title     string          `json:"title"`
	Year      int             `json:"year,omitzero"`
	// IDs are the title's on the provider whose search found it.
	IDs map[domain.Provider]string `json:"ids"`
	// Seasons are a show's asked for; none is every one.
	Seasons []int               `json:"seasons,omitzero"`
	State   domain.RequestState `json:"state"`
	// Reason is why it was declined, where the one who declined it said.
	Reason      string    `json:"reason,omitzero"`
	DecidedByID uuid.UUID `json:"decided_by_id,omitzero"`
	AskedAt     time.Time `json:"asked_at"`
	DecidedAt   time.Time `json:"decided_at,omitzero"`
	// TitleID is the title a library holds of it once it is available, to open as any other.
	TitleID     uuid.UUID `json:"title_id,omitzero"`
	AvailableAt time.Time `json:"available_at,omitzero"`
}

func requestOf(r store.Request) requestJSON {
	return requestJSON{
		ID: r.ID, ProfileID: r.Profile, Profile: r.ProfileName, LibraryID: r.Library, Kind: r.Kind,
		Title: r.Title, Year: r.Year, IDs: map[domain.Provider]string{r.Provider: r.Value}, Seasons: r.Seasons,
		State: r.State, Reason: r.Reason, DecidedByID: r.DecidedBy, AskedAt: r.AskedAt, DecidedAt: r.DecidedAt,
		TitleID: r.Item, AvailableAt: r.AvailableAt,
	}
}

func requestsOf(rs []store.Request) []requestJSON {
	out := make([]requestJSON, len(rs))
	for i, r := range rs {
		out[i] = requestOf(r)
	}
	return out
}

// askJSON is a title a search answered as requestable, by its id there, and of a show the seasons
// asked for, every one where none is.
type askJSON struct {
	RequestableID uuid.UUID `json:"requestable_id"`
	Seasons       []int     `json:"seasons,omitzero"`
}

type declineJSON struct {
	// Reason is why, for the one who asked; optional.
	Reason string `json:"reason,omitzero"`
}

// askFor asks for a title a library of files' search found, as the profile signed in.
func (a *API) askFor(w http.ResponseWriter, r *http.Request) {
	var req askJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.RequestableID == (uuid.UUID{}) {
		writeProblem(w, a.logger, codeInvalidBody, "requestable_id is a title a search answered as requestable")
		return
	}
	if slices.ContainsFunc(req.Seasons, func(n int) bool { return n < 0 }) {
		writeProblem(w, a.logger, codeInvalidBody, "seasons are numbers from 0, a show's specials")
		return
	}
	asked, err := a.svc.Requests.AskFor(r.Context(), auth.SessionOf(r.Context()).Profile.ID, req.RequestableID, req.Seasons)
	if a.answeredAs(w, r, err, "requestable_id is a title a search answered as requestable, found in the last day") {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, requestOf(asked))
}

// ownRequests answers a page of the signed-in profile's requests, newest first.
func (a *API) ownRequests(w http.ResponseWriter, r *http.Request) {
	offset, limit, ok := a.paging(w, r, maxRequestsLimit)
	if !ok {
		return
	}
	rs, total, err := a.svc.Requests.Requests(r.Context(), auth.SessionOf(r.Context()).Profile.ID, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[requestJSON]{requestsOf(rs), offset, total})
}

func (a *API) withdrawRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Requests.WithdrawRequest(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// keptRequests answers a page of the requests of the profiles an admin or manager keeps, newest
// first.
func (a *API) keptRequests(w http.ResponseWriter, r *http.Request) {
	state, ok := queryEnum(a, w, r, "state", "", domain.RequestStates())
	if !ok {
		return
	}
	offset, limit, ok := a.paging(w, r, maxRequestsLimit)
	if !ok {
		return
	}
	rs, total, err := a.svc.Requests.RequestsKept(r.Context(), keeper(r), state, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[requestJSON]{requestsOf(rs), offset, total})
}

func (a *API) approveRequest(w http.ResponseWriter, r *http.Request) {
	a.decideRequest(w, r, domain.RequestApproved, "")
}

func (a *API) declineRequest(w http.ResponseWriter, r *http.Request) {
	var req declineJSON
	if !a.decode(w, r, &req) {
		return
	}
	a.decideRequest(w, r, domain.RequestDeclined, req.Reason)
}

func (a *API) decideRequest(w http.ResponseWriter, r *http.Request, state domain.RequestState, reason string) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	decided, err := a.svc.Requests.DecideRequest(r.Context(), id, keeper(r), auth.SessionOf(r.Context()).Profile.ID, state, reason)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, requestOf(decided))
}

func (a *API) requestsRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/requests", access: signedIn,
			summary: "Ask for a title a search answered as requestable; an admin's is approved at once",
			body:    askJSON{}, status: http.StatusCreated, reply: requestJSON{}, handle: a.askFor,
		},
		{
			pattern: "GET /api/v1/requests", access: signedIn, summary: "Page the profile's requests, newest first",
			query: pageParams, status: http.StatusOK, reply: pageJSON[requestJSON]{}, handle: a.ownRequests,
		},
		{
			pattern: "DELETE /api/v1/requests/{id}", access: signedIn, summary: "Take back one of the profile's requests while it waits",
			status: http.StatusNoContent, handle: a.withdrawRequest,
		},
		{
			pattern: "GET /api/v1/admin/requests", access: manages,
			summary: "Page the requests of the profiles one keeps, newest first: every profile's for an admin",
			query:   append([]param{{"state", domain.RequestState(""), "Only requests in this state."}}, pageParams...),
			status:  http.StatusOK, reply: pageJSON[requestJSON]{}, handle: a.keptRequests,
		},
		{
			pattern: "POST /api/v1/admin/requests/{id}/approve", access: manages, summary: "Approve a request while it waits",
			status: http.StatusOK, reply: requestJSON{}, handle: a.approveRequest,
		},
		{
			pattern: "POST /api/v1/admin/requests/{id}/decline", access: manages, summary: "Decline a request while it waits, saying why",
			body: declineJSON{}, status: http.StatusOK, reply: requestJSON{}, handle: a.declineRequest,
		},
	}
}
