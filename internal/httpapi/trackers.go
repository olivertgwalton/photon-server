package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/tracker"
)

type trackerLinks interface {
	Trackers(ctx context.Context, profile uuid.UUID) ([]tracker.Status, error)
	Link(ctx context.Context, profile uuid.UUID, t domain.Tracker) (kv.TrackerLink, error)
	Unlink(ctx context.Context, profile uuid.UUID, t domain.Tracker) error
}

type trackerClients interface {
	TrackerClients(ctx context.Context) (map[domain.Tracker]string, error)
	SetTrackerClient(ctx context.Context, t domain.Tracker, clientID string) error
}

// trackerClientJSON is the app an admin registered on a tracker, which every profile links its
// account through; an empty client id is a tracker no profile can link.
type trackerClientJSON struct {
	Tracker  domain.Tracker `json:"tracker"`
	ClientID string         `json:"client_id"`
}

type trackerClientChangeJSON struct {
	ClientID string `json:"client_id"`
}

// trackerCodeJSON is a code for a profile to enter at VerificationURI on the tracker's site, and
// VerificationURIComplete the same with the code filled in, for a QR code (RFC 8628 3.3.1). The
// server asks the tracker after it, and the profile's events say when it is linked or expires.
type trackerCodeJSON struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresInMS             int64  `json:"expires_in_ms"`
}

type profileTrackerJSON struct {
	Tracker domain.Tracker `json:"tracker"`
	State   tracker.State  `json:"state"`
	// Username and LinkedAt are the account linked.
	Username string    `json:"username,omitzero"`
	LinkedAt time.Time `json:"linked_at,omitzero"`
	// Code is what the profile enters to link one.
	Code *trackerCodeJSON `json:"code,omitzero"`
}

func trackerCodeOf(l kv.TrackerLink) trackerCodeJSON {
	return trackerCodeJSON{
		UserCode: l.UserCode, VerificationURI: l.VerificationURI, VerificationURIComplete: l.VerificationURIComplete,
		ExpiresInMS: max(time.Until(l.Expires), 0).Milliseconds(),
	}
}

// maxClientID is the longest client id taken: Trakt's and Simkl's are 64 characters.
const maxClientID = 200

func (a *API) adminTrackers(w http.ResponseWriter, r *http.Request) {
	clients, err := a.svc.TrackerClients.TrackerClients(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := make([]trackerClientJSON, len(domain.Trackers()))
	for i, t := range domain.Trackers() {
		out[i] = trackerClientJSON{Tracker: t, ClientID: clients[t]}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[trackerClientJSON]{out})
}

func (a *API) setTrackerClient(w http.ResponseWriter, r *http.Request) {
	t, ok := a.pathTracker(w, r)
	if !ok {
		return
	}
	var req trackerClientChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	if len(req.ClientID) > maxClientID || strings.ContainsFunc(req.ClientID, unicode.IsSpace) {
		writeProblem(w, a.logger, codeInvalidBody, "client_id is the client id of the app registered on the tracker, with no spaces")
		return
	}
	if a.answered(w, r, a.svc.TrackerClients.SetTrackerClient(r.Context(), t, req.ClientID)) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, trackerClientJSON{Tracker: t, ClientID: req.ClientID})
}

// ownTrackers answers how far the profile is from an account on each tracker.
func (a *API) ownTrackers(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Trackers.Trackers(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if a.answered(w, r, err) {
		return
	}
	out := make([]profileTrackerJSON, len(all))
	for i, s := range all {
		out[i] = profileTrackerJSON{Tracker: s.Tracker, State: s.State}
		switch s.State {
		case tracker.StateLinked:
			out[i].Username, out[i].LinkedAt = s.Account.Username, s.Account.LinkedAt.UTC()
		case tracker.StateLinking:
			code := trackerCodeOf(s.Link)
			out[i].Code = &code
		case tracker.StateUnavailable, tracker.StateUnlinked:
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[profileTrackerJSON]{out})
}

func (a *API) linkTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.pathTracker(w, r)
	if !ok {
		return
	}
	link, err := a.svc.Trackers.Link(r.Context(), auth.SessionOf(r.Context()).Profile.ID, t)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, trackerCodeOf(link))
}

func (a *API) unlinkTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.pathTracker(w, r)
	if !ok {
		return
	}
	if a.answeredAs(w, r, a.svc.Trackers.Unlink(r.Context(), auth.SessionOf(r.Context()).Profile.ID, t), "the profile has linked no account there") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathTracker reads the tracker a path names; any other is a path to nothing.
func (a *API) pathTracker(w http.ResponseWriter, r *http.Request) (domain.Tracker, bool) {
	t := domain.Tracker(r.PathValue("tracker"))
	if !slices.Contains(domain.Trackers(), t) {
		writeProblem(w, a.logger, codeNotFound, "")
		return "", false
	}
	return t, true
}

var trackerParam = param{"tracker", domain.Tracker(""), "The tracker."}

func (a *API) trackersRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/trackers", access: admin,
			summary: "List the trackers profiles may link accounts on, and the client id of the app registered on each",
			status:  http.StatusOK, reply: listJSON[trackerClientJSON]{}, handle: a.adminTrackers,
		},
		{
			pattern: "PUT /api/v1/admin/trackers/{tracker}", access: admin,
			summary: "Set the client id of the app registered on a tracker, which profiles link their accounts through; empty, none can",
			path:    []param{trackerParam}, body: trackerClientChangeJSON{}, status: http.StatusOK, reply: trackerClientJSON{},
			handle: a.setTrackerClient,
		},
		{
			pattern: "GET /api/v1/profile/trackers", access: signedIn,
			summary: "The profile's account on each tracker, or the code it is entering to link one",
			status:  http.StatusOK, reply: listJSON[profileTrackerJSON]{}, handle: a.ownTrackers,
		},
		{
			pattern: "POST /api/v1/profile/trackers/{tracker}/link", access: signedIn,
			summary: "Ask a tracker for a code for the profile to enter there, in place of any before; tracker.changed says when it is linked or expires",
			path:    []param{trackerParam}, status: http.StatusOK, reply: trackerCodeJSON{}, handle: a.linkTracker,
		},
		{
			pattern: "DELETE /api/v1/profile/trackers/{tracker}", access: signedIn,
			summary: "Unlink the profile's account on a tracker, or stop linking one, and have the tracker forget what it granted",
			path:    []param{trackerParam}, status: http.StatusNoContent, handle: a.unlinkTracker,
		},
	}
}
