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
	Grant(ctx context.Context, profile uuid.UUID, t domain.Tracker, token string) error
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
	VerificationURI         uri    `json:"verification_uri"`
	VerificationURIComplete uri    `json:"verification_uri_complete"`
	ExpiresInMS             int64  `json:"expires_in_ms"`
}

type profileTrackerJSON struct {
	Tracker domain.Tracker `json:"tracker"`
	// Flow is how an account is linked: by a code entered on the tracker's site, or by a token its
	// site shows at VerificationURI, brought back.
	Flow  tracker.LinkFlow `json:"flow"`
	State tracker.State    `json:"state"`
	// Username and LinkedAt are the account linked.
	Username string    `json:"username,omitzero"`
	LinkedAt time.Time `json:"linked_at,omitzero"`
	// Code is what the profile enters to link one.
	Code *trackerCodeJSON `json:"code,omitzero"`
}

func trackerCodeOf(l kv.TrackerLink) trackerCodeJSON {
	return trackerCodeJSON{
		UserCode: l.UserCode, VerificationURI: uri(l.VerificationURI), VerificationURIComplete: uri(l.VerificationURIComplete),
		ExpiresInMS: max(time.Until(l.Expires), 0).Milliseconds(),
	}
}

// maxClientID is the longest client id taken: Trakt's and Simkl's are 64 characters, MDBList's 40.
const maxClientID = 200

func (a *API) adminTrackers(r *http.Request, _ none) (listJSON[trackerClientJSON], error) {
	clients, err := a.svc.TrackerClients.TrackerClients(r.Context())
	if err != nil {
		return listJSON[trackerClientJSON]{}, err
	}
	out := make([]trackerClientJSON, len(domain.Trackers()))
	for i, t := range domain.Trackers() {
		out[i] = trackerClientJSON{Tracker: t, ClientID: clients[t]}
	}
	return listJSON[trackerClientJSON]{out}, nil
}

func (a *API) setTrackerClient(r *http.Request, req trackerClientChangeJSON) (trackerClientJSON, error) {
	t, err := pathTracker(r)
	if err != nil {
		return trackerClientJSON{}, err
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	if len(req.ClientID) > maxClientID || strings.ContainsFunc(req.ClientID, unicode.IsSpace) {
		return trackerClientJSON{}, refuse(codeInvalidBody, "client_id is the client id of the app registered on the tracker, with no spaces")
	}
	if err := a.svc.TrackerClients.SetTrackerClient(r.Context(), t, req.ClientID); err != nil {
		return trackerClientJSON{}, err
	}
	return trackerClientJSON{Tracker: t, ClientID: req.ClientID}, nil
}

// ownTrackers answers how far the profile is from an account on each tracker.
func (a *API) ownTrackers(r *http.Request, _ none) (listJSON[profileTrackerJSON], error) {
	all, err := a.svc.Trackers.Trackers(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if err != nil {
		return listJSON[profileTrackerJSON]{}, err
	}
	out := make([]profileTrackerJSON, len(all))
	for i, s := range all {
		out[i] = profileTrackerJSON{Tracker: s.Tracker, Flow: s.Flow, State: s.State}
		switch s.State {
		case tracker.StateLinked:
			out[i].Username, out[i].LinkedAt = s.Account.Username, s.Account.LinkedAt.UTC()
		case tracker.StateLinking:
			code := trackerCodeOf(s.Link)
			out[i].Code = &code
		case tracker.StateUnavailable, tracker.StateUnlinked:
		}
	}
	return listJSON[profileTrackerJSON]{out}, nil
}

func (a *API) linkTracker(r *http.Request, _ none) (trackerCodeJSON, error) {
	t, err := pathTracker(r)
	if err != nil {
		return trackerCodeJSON{}, err
	}
	link, err := a.svc.Trackers.Link(r.Context(), auth.SessionOf(r.Context()).Profile.ID, t)
	if err != nil {
		return trackerCodeJSON{}, err
	}
	return trackerCodeOf(link), nil
}

type trackerTokenJSON struct {
	Token string `json:"token"`
}

// maxToken bounds a token brought back; AniList's are about a thousand bytes.
const maxToken = 4096

// grantTracker links the profile's account on a tracker linked by token, by the token its site
// showed the profile.
func (a *API) grantTracker(r *http.Request, req trackerTokenJSON) (none, error) {
	t, err := pathTracker(r)
	if err != nil {
		return none{}, err
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" || len(req.Token) > maxToken {
		return none{}, refuse(codeInvalidBody, "token is the token the tracker's site showed")
	}
	return none{}, a.svc.Trackers.Grant(r.Context(), auth.SessionOf(r.Context()).Profile.ID, t, req.Token)
}

func (a *API) unlinkTracker(r *http.Request, _ none) (none, error) {
	t, err := pathTracker(r)
	if err != nil {
		return none{}, err
	}
	return none{}, notFoundAs(a.svc.Trackers.Unlink(r.Context(), auth.SessionOf(r.Context()).Profile.ID, t), "the profile has linked no account there")
}

// pathTracker reads the tracker a path names; any other is a path to nothing.
func pathTracker(r *http.Request) (domain.Tracker, error) {
	t := domain.Tracker(r.PathValue("tracker"))
	if !slices.Contains(domain.Trackers(), t) {
		return "", refuse(codeNotFound, "")
	}
	return t, nil
}

var trackerParam = param{"tracker", domain.Tracker(""), "The tracker."}

func (a *API) trackersRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/trackers", access: admin,
			summary: "List the trackers profiles may link accounts on, and the client id of the app registered on each",
			status:  http.StatusOK,
		}, a.adminTrackers),
		a.handles(route{
			pattern: "PUT /api/v1/admin/trackers/{tracker}", access: admin,
			summary: "Set the client id of the app registered on a tracker, which profiles link their accounts through; empty, none can",
			path:    []param{trackerParam}, status: http.StatusOK,
		}, a.setTrackerClient),
		a.handles(route{
			pattern: "GET /api/v1/profile/trackers", access: signedIn,
			summary: "The profile's account on each tracker, or the code it is entering to link one",
			status:  http.StatusOK,
		}, a.ownTrackers),
		a.handles(route{
			pattern: "POST /api/v1/profile/trackers/{tracker}/link", access: signedIn,
			summary: "Ask a tracker for a code for the profile to enter there, in place of any before; tracker.changed says when it is linked or expires",
			path:    []param{trackerParam}, status: http.StatusOK,
		}, a.linkTracker),
		a.handles(route{
			pattern: "PUT /api/v1/profile/trackers/{tracker}/token", access: signedIn,
			summary: "Link the profile's account on a tracker linked by token, by the token its site showed",
			path:    []param{trackerParam}, status: http.StatusNoContent,
		}, a.grantTracker),
		a.handles(route{
			pattern: "DELETE /api/v1/profile/trackers/{tracker}", access: signedIn,
			summary: "Unlink the profile's account on a tracker, or stop linking one, and have the tracker forget what it granted",
			path:    []param{trackerParam}, status: http.StatusNoContent,
		}, a.unlinkTracker),
	}
}
