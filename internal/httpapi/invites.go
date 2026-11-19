package httpapi

import (
	"cmp"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// An invite is how a profile added without a password gets one: its keeper sends the code, as a
// link, to the profile's owner, who chooses the password and is signed in. Only the code's holder
// learns the password, and it serves a profile that has none, so a code that leaks after it is
// accepted opens nothing.

// inviteJSON is the address to send an invite as: the web app's invite page at the public URL,
// its code in the fragment, which a browser never sends, so no server or proxy logs it. The server
// keeps only the code's hash, so this is answered once.
type inviteJSON struct {
	URL         uri   `json:"url"`
	ExpiresInMS int64 `json:"expires_in_ms"`
}

// acceptanceJSON is an invite's code, the password chosen, and the device choosing it, which is
// signed in as the profile, kept as a sign-in keeps it.
type acceptanceJSON struct {
	Code     string      `json:"code"`
	Password string      `json:"password"`
	Device   string      `json:"device"`
	Client   string      `json:"client"`
	Keep     domain.Keep `json:"keep,omitzero"`
}

func (a *API) invite(r *http.Request, _ none) (inviteJSON, error) {
	code, err := a.svc.Auth.Invite(r.Context(), pathUUID(r, "id"), keeper(r))
	if err != nil {
		return inviteJSON{}, err
	}
	link := a.publicURL(r).JoinPath("/auth/invite")
	link.Fragment = code
	return inviteJSON{URL: uri(link.String()), ExpiresInMS: auth.InviteTTL.Milliseconds()}, nil
}

func (a *API) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptanceJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Code == "" || req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, "code, device and client are required")
		return
	}
	if !a.allowed(w, r, auth.InvitesPerAddress, a.addrKey(r, "invite")) {
		return
	}
	token, profile, err := a.svc.Auth.AcceptInvite(r.Context(), req.Code, req.Password, auth.Device{Name: req.Device, Client: req.Client})
	if a.answered(w, r, err) {
		return
	}
	// The device is signed in, so the activity log says so as it says any sign-in.
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSignedIn, Profile: profile.ID, Details: domain.SignInDetails{
		Name: profile.Name, Device: req.Device, Client: req.Client, Address: a.svc.Reach.Client(r).String(),
	}})
	a.signedIn(w, r, cmp.Or(req.Keep, domain.KeepToken), token, profile)
}

func (a *API) inviteRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "POST /api/v1/admin/profiles/{id}/invites", access: manages,
			summary: "Invite a profile without a password to choose one; its address is answered this once",
			status:  http.StatusCreated,
		}, a.invite),
		{
			pattern: "POST /api/v1/auth/invites/redemptions", access: public,
			summary: "Choose the password an invite is for and sign this device in as its profile",
			body:    acceptanceJSON{}, status: http.StatusOK, reply: loginResponse{}, handle: a.acceptInvite,
		},
	}
}
