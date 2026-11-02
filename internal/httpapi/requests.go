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
	AskFor(ctx context.Context, profile, discovery uuid.UUID, seasons []int) (store.Request, []store.Told, error)
	Requests(ctx context.Context, profile uuid.UUID, page store.Page) ([]store.Request, store.Extent, error)
	RequestsKept(ctx context.Context, by *uuid.UUID, state domain.RequestState, page store.Page) ([]store.Request, store.Extent, error)
	WithdrawRequest(ctx context.Context, profile, id uuid.UUID) ([]store.Cleared, error)
	DecideRequest(ctx context.Context, id uuid.UUID, by *uuid.UUID, decider uuid.UUID, state domain.RequestState, reason string) (store.Request, []store.Cleared, error)
}

// requestablePages draws a title a profile may ask for as a title's page.
type requestablePages interface {
	Page(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
}

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
func (a *API) askFor(r *http.Request, req askJSON) (requestJSON, error) {
	if req.RequestableID == (uuid.UUID{}) {
		return requestJSON{}, refuse(codeInvalidBody, "requestable_id is a title a search answered as requestable")
	}
	if slices.ContainsFunc(req.Seasons, func(n int) bool { return n < 0 }) {
		return requestJSON{}, refuse(codeInvalidBody, "seasons are numbers from 0, a show's specials")
	}
	asked, told, err := a.svc.Requests.AskFor(r.Context(), auth.SessionOf(r.Context()).Profile.ID, req.RequestableID, req.Seasons)
	if err != nil {
		return requestJSON{}, notFoundAs(err, "requestable_id is a title a search answered as requestable, found in the last day")
	}
	for _, t := range told {
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNotificationsChanged, Profile: t.ProfileID, Details: domain.NotificationsDetails{
			Change: domain.NotificationsAdded, IDs: t.IDs,
		}})
	}
	return requestOf(asked), nil
}

// ownRequests answers a page of the signed-in profile's requests, newest first.
func (a *API) ownRequests(r *http.Request, _ none) (pageJSON[requestJSON], error) {
	page, err := paging(r, defaultWallLimit)
	if err != nil {
		return pageJSON[requestJSON]{}, err
	}
	rs, extent, err := a.svc.Requests.Requests(r.Context(), auth.SessionOf(r.Context()).Profile.ID, page)
	if err != nil {
		return pageJSON[requestJSON]{}, err
	}
	return pageAt(each(rs, requestOf), page.Offset, extent), nil
}

func (a *API) withdrawRequest(r *http.Request, _ none) (none, error) {
	cleared, err := a.svc.Requests.WithdrawRequest(r.Context(), auth.SessionOf(r.Context()).Profile.ID, pathUUID(r, "id"))
	if err != nil {
		return none{}, err
	}
	a.raiseCleared(r.Context(), cleared)
	return none{}, nil
}

// raiseCleared tells each profile's devices that the notifications of a request that no longer
// waits are gone from its inbox.
func (a *API) raiseCleared(ctx context.Context, cleared []store.Cleared) {
	for _, c := range cleared {
		a.svc.Events.Raise(ctx, domain.Event{Kind: domain.EventNotificationsChanged, Profile: c.ProfileID, Details: domain.NotificationsDetails{
			Change: domain.NotificationsCleared, IDs: c.IDs, Unread: c.Unread,
		}})
	}
}

// keptRequests answers a page of the requests of the profiles an admin or manager keeps, newest
// first.
func (a *API) keptRequests(r *http.Request, _ none) (pageJSON[requestJSON], error) {
	state, err := queryEnum(r, "state", "", domain.RequestStates())
	if err != nil {
		return pageJSON[requestJSON]{}, err
	}
	page, err := paging(r, defaultWallLimit)
	if err != nil {
		return pageJSON[requestJSON]{}, err
	}
	rs, extent, err := a.svc.Requests.RequestsKept(r.Context(), keeper(r), state, page)
	if err != nil {
		return pageJSON[requestJSON]{}, err
	}
	return pageAt(each(rs, requestOf), page.Offset, extent), nil
}

func (a *API) approveRequest(r *http.Request, _ none) (requestJSON, error) {
	return a.decideRequest(r, domain.RequestApproved, "")
}

func (a *API) declineRequest(r *http.Request, req declineJSON) (requestJSON, error) {
	return a.decideRequest(r, domain.RequestDeclined, req.Reason)
}

func (a *API) decideRequest(r *http.Request, state domain.RequestState, reason string) (requestJSON, error) {
	decided, cleared, err := a.svc.Requests.DecideRequest(r.Context(), pathUUID(r, "id"), keeper(r), auth.SessionOf(r.Context()).Profile.ID, state, reason)
	if err != nil {
		return requestJSON{}, err
	}
	a.raiseCleared(r.Context(), cleared)
	return requestOf(decided), nil
}

func (a *API) requestsRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "POST /api/v1/requests", access: signedIn,
			summary: "Ask for a title a search answered as requestable; an admin's is approved at once",
			status:  http.StatusCreated,
		}, a.askFor),
		a.handles(route{
			pattern: "GET /api/v1/requests", access: signedIn, summary: "Page the profile's requests, newest first",
			query: pageParams, status: http.StatusOK,
		}, a.ownRequests),
		a.handles(route{
			pattern: "DELETE /api/v1/requests/{id}", access: signedIn, summary: "Take back one of the profile's requests while it waits",
			status: http.StatusNoContent,
		}, a.withdrawRequest),
		a.handles(route{
			pattern: "GET /api/v1/admin/requests", access: manages,
			summary: "Page the requests of the profiles one keeps, newest first: every profile's for an admin",
			query:   append([]param{{"state", domain.RequestState(""), "Only requests in this state."}}, pageParams...),
			status:  http.StatusOK,
		}, a.keptRequests),
		a.handles(route{
			pattern: "POST /api/v1/admin/requests/{id}/approve", access: manages, summary: "Approve a request while it waits",
			status: http.StatusOK,
		}, a.approveRequest),
		a.handles(route{
			pattern: "POST /api/v1/admin/requests/{id}/decline", access: manages, summary: "Decline a request while it waits, saying why",
			status: http.StatusOK,
		}, a.declineRequest),
	}
}
