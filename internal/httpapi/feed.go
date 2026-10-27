package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/websocket"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// audience says what of the libraries and titles a profile may be told of.
type audience interface {
	HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error)
	Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error)
	SameTitles(ctx context.Context, profile, title uuid.UUID) ([]uuid.UUID, error)
	Keeps(ctx context.Context, profile uuid.UUID, by *uuid.UUID) (bool, error)
}

// feed is what the events socket tells: every client what changes of what its profile sees, and
// an admin who asks what happens on any node besides.
type feed string

const (
	feedProfile feed = "profile"
	feedServer  feed = "server"
)

func feeds() []feed { return []feed{feedProfile, feedServer} }

const (
	// pingEvery is how often the events socket is pinged, so a proxy that closes an idle
	// connection, after a minute in nginx's default, leaves it open.
	pingEvery = 15 * time.Second
	// lostAfter is how long a client may answer no ping before its socket is closed.
	lostAfter = 4 * pingEvery
)

// socketMessage is one message on the events socket: an event of a feed, named by its kind, or the
// feed's first, which says what is going on now.
type socketMessage struct {
	Feed  feed   `json:"feed"`
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type helloJSON struct {
	Scans []scanJSON `json:"scans"`
}

// eventsSocket is what events sends: on each feed its first message, then the events it tells,
// named by their kinds.
func eventsSocket() asSocket {
	profile := map[string]any{"hello": helloJSON{}}
	for _, k := range []domain.EventKind{
		domain.EventLibraryChanged, domain.EventTitleUpdated, domain.EventUserDataChanged, domain.EventScanProgress,
		domain.EventLibraryScanned, domain.EventPlaybackStopped, domain.EventRestoreStarted, domain.EventTrackerChanged,
		domain.EventRequestAdded, domain.EventRequestChanged, domain.EventRequestRemoved,
	} {
		profile[string(k)] = eventJSON{}
	}
	profile[string(domain.EventNotificationsChanged)] = inboxChangeJSON{}
	server := map[string]any{"snapshot": snapshotJSON{}}
	for _, k := range domain.EventKinds() {
		server[string(k)] = eventJSON{}
	}
	return asSocket{feedProfile: profile, feedServer: server}
}

// events keeps a WebSocket open that tells what happens on every node, as Plex's, Emby's and
// Jellyfin's clients each keep one: on the profile feed, what changes of what the profile sees, so
// a client's pages stay right without asking again, as Jellyfin's LibraryChanged and
// UserDataChanged; its own playbacks stopping, so a player closes; what came of a code it was
// entering on a tracker; and the requests it asked for, and those of the profiles it keeps, so
// whoever decides them hears one is waiting. An admin who asks is told on the server feed what happens on any node,
// as Plex's activity panel is. One socket carries both, so a browser on HTTP/1.1, which opens few
// connections to a server, keeps the rest for its pages.
// Each feed begins with what is going on now: the profile's with a hello naming the scans going
// on, the server's with a snapshot, so a dashboard needs no second request. Nothing is kept to
// resend, so a client that reconnects asks again for what it shows.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	ctx, session := r.Context(), auth.SessionOf(r.Context())
	profile := session.Profile.ID
	want, ok := queryEnum(a, w, r, "feed", feedProfile, feeds())
	if !ok {
		return
	}
	if want == feedServer && session.Profile.Role != domain.RoleAdmin {
		writeProblem(w, a.logger, codeForbidden, "only an admin may")
		return
	}
	said := words.Negotiate(w, r)
	events, stop := a.svc.Events.Subscribe()
	defer stop()
	// Subscribed first, so nothing between what is going on and the socket is missed; what arrives
	// twice is a state the client already shows.
	first := []socketMessage{}
	hello, err := a.hello(ctx, profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	first = append(first, socketMessage{feedProfile, "hello", hello})
	var tell func(domain.Event) eventJSON
	if want == feedServer {
		now, err := a.snapshot(ctx)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		names, err := a.eventNames(ctx)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		first = append(first, socketMessage{feedServer, "snapshot", now})
		tell = func(e domain.Event) eventJSON {
			out := eventOf(e)
			out.Text = said.Event(e, names)
			return out
		}
	}
	c, err := websocket.Accept(w, r)
	if err != nil {
		a.logger.DebugContext(ctx, "events socket not opened", slog.Any("err", err))
		return
	}
	defer func() {
		if err := c.Close(websocket.StatusGoingAway); err != nil {
			a.logger.DebugContext(ctx, "events socket not closed cleanly", slog.Any("err", err))
		}
	}()
	// The client says nothing but its answers to pings, which Read takes; it is gone once it has
	// answered none for lostAfter.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, err := c.Read(lostAfter); err != nil {
				return
			}
		}
	}()
	for _, m := range first {
		if !a.sendMessage(ctx, c, m) {
			return
		}
	}
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-gone:
			return
		case <-ping.C:
			if c.Ping() != nil {
				return
			}
		case e, open := <-events:
			if !open {
				return
			}
			told, send, err := a.toldTo(ctx, session.Profile, said, e)
			if err != nil {
				// The client reconnects and asks again for what it shows.
				a.logger.WarnContext(ctx, "events socket ended", slog.Any("err", err))
				return
			}
			if send && !a.sendMessage(ctx, c, socketMessage{feedProfile, string(e.Kind), told}) {
				return
			}
			if tell != nil && !a.sendMessage(ctx, c, socketMessage{feedServer, string(e.Kind), tell(e)}) {
				return
			}
		}
	}
}

// hello is what the profile feed begins with: the scans going on in the libraries the profile sees.
func (a *API) hello(ctx context.Context, profile uuid.UUID) (helloJSON, error) {
	out := helloJSON{Scans: []scanJSON{}}
	scans, err := a.svc.Events.Scans(ctx)
	if err != nil {
		return out, err
	}
	for _, s := range scans {
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, s.Library)
		if err != nil {
			return out, err
		}
		if ok {
			out.Scans = append(out.Scans, scanJSON{LibraryID: s.Library, Phase: s.Phase, Done: s.Done, Known: s.Known})
		}
	}
	return out, nil
}

// sendMessage writes a message on the events socket, answering whether the client is still there.
func (a *API) sendMessage(ctx context.Context, c *websocket.Conn, m socketMessage) bool {
	body, err := json.Marshal(m)
	if err != nil {
		a.logger.ErrorContext(ctx, "events message not encoded", slog.Any("err", err))
		return false
	}
	return c.Write(body) == nil
}

// toldTo answers an event as a profile is told it, and whether it is: its own state's changes,
// libraries and titles it may see, and the requests of the profiles it keeps.
func (a *API) toldTo(ctx context.Context, viewer domain.Profile, said words.Words, e domain.Event) (any, bool, error) {
	profile := viewer.ID
	switch e.Kind {
	case domain.EventUserDataChanged:
		if e.Profile != profile {
			return eventJSON{}, false, nil
		}
		// A profile's state of a title is its state wherever the title is listed, so each place
		// is named.
		if e.Item != (uuid.UUID{}) {
			same, err := a.svc.Audience.SameTitles(ctx, profile, e.Item)
			if err != nil {
				return eventJSON{}, false, err
			}
			e.Details = domain.UserDataDetails{TitleIDs: nonNil(same)}
		}
		return eventOf(e), true, nil
	case domain.EventTitleUpdated:
		seen, err := a.svc.Audience.Visible(ctx, profile, []uuid.UUID{e.Item})
		return eventOf(e), len(seen) == 1, err
	case domain.EventRestoreStarted:
		return eventOf(e), true, nil
	case domain.EventTrackerChanged:
		return eventOf(e), e.Profile == profile, nil
	case domain.EventRequestAdded, domain.EventRequestChanged, domain.EventRequestRemoved:
		// The one who asked sees it in its requests, and whoever decides it in theirs to decide.
		by, keeps := keeperOf(domain.Session{Profile: viewer})
		if e.Profile == profile || !keeps {
			return eventOf(e), e.Profile == profile, nil
		}
		kept, err := a.svc.Audience.Keeps(ctx, e.Profile, by)
		return eventOf(e), kept, err
	case domain.EventNotificationsChanged:
		d, _ := e.Details.(domain.NotificationsDetails)
		if e.Profile != profile {
			return nil, false, nil
		}
		change, err := a.inboxChange(ctx, profile, said, d)
		return change, err == nil, err
	case domain.EventScanProgress, domain.EventLibraryScanned:
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, e.Library)
		return eventOf(e), ok, err
	case domain.EventLibraryChanged:
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, e.Library)
		if err != nil || !ok {
			return eventJSON{}, false, err
		}
		gathered, _ := e.Details.(domain.LibraryChangedDetails)
		details, told := domain.LibraryChangedDetails{}, false
		for _, change := range domain.TitleChanges() {
			ids := gathered[change]
			// A title removed is no longer there to ask of; its id says nothing of it.
			if change != domain.TitleRemoved {
				if ids, err = a.svc.Audience.Visible(ctx, profile, ids); err != nil {
					return eventJSON{}, false, err
				}
			}
			details[change] = nonNil(ids)
			told = told || len(ids) > 0
		}
		e.Details = details
		return eventOf(e), told, nil
	case domain.EventPlaybackStopped:
		// A profile's player is told its playback stopped, wherever that was, so it closes, as
		// Jellyfin's dashboard sends its session a stop. The card, with the device's name and
		// address, is the dashboard's.
		if e.Profile != profile {
			return eventJSON{}, false, nil
		}
		shown, _ := e.Details.(domain.PlaybackDetails)
		e.Details = domain.PlaybackClosedDetails{PlaybackID: shown.Playback.ID}
		return eventOf(e), true, nil
	case domain.EventPlaybackStarted, domain.EventPlaybackPaused, domain.EventPlaybackResumed,
		domain.EventSignedIn, domain.EventSignInRefused,
		domain.EventProfileAdded, domain.EventProfileRemoved, domain.EventLibraryAdded,
		domain.EventLibraryRemoved, domain.EventTitlesAdded,
		domain.EventTaskStarted, domain.EventTaskFinished, domain.EventTaskFailed, domain.EventBackupMade,
		domain.EventJobStarted, domain.EventJobFinished, domain.EventJobFailed, domain.EventJobDead,
		domain.EventJobsProgress, domain.EventWebhookTest, domain.EventMaintenanceChanged,
		domain.EventNetworkChanged, domain.EventStorageChanged, domain.EventNodesChanged, domain.EventServerChanged:
	}
	return eventJSON{}, false, nil
}

func (a *API) feedRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/events", access: signedIn,
			summary: "Open a WebSocket telling what changes of the libraries, titles and state the profile sees, its playbacks stopping, its trackers linking and the requests it asked for or decides, and an admin who asks what happens on any node; each message is a JSON object of its feed, event and data",
			query:   []param{{"feed", feedProfile, "profile tells what the profile sees; server tells an admin what happens on any node besides."}},
			status:  http.StatusSwitchingProtocols, reply: eventsSocket(), handle: a.events,
		},
	}
}
