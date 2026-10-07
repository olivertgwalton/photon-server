package httpapi

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// audience says what of the libraries and titles a profile may be told of.
type audience interface {
	HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error)
	Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error)
	SameTitles(ctx context.Context, profile, title uuid.UUID) ([]uuid.UUID, error)
}

type helloJSON struct {
	Scans []scanJSON `json:"scans"`
}

// feedStream is what events sends: hello, then the events a profile is told, named by their kinds.
func feedStream() asStream {
	out := asStream{"hello": helloJSON{}}
	for _, k := range []domain.EventKind{
		domain.EventLibraryChanged, domain.EventTitleUpdated, domain.EventUserDataChanged, domain.EventScanProgress,
		domain.EventLibraryScanned, domain.EventPlaybackStopped,
	} {
		out[string(k)] = eventJSON{}
	}
	return out
}

// events streams what changes of what the profile sees, from every node, as Server-Sent Events,
// so a client's pages stay right without asking again: as Jellyfin's LibraryChanged and
// UserDataChanged, and Plex's notifications; and its own playbacks stopping, so a player closes.
// First a hello with the scans going on. Nothing is kept to resend, so a client that reconnects
// asks again for what it shows.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	ctx, profile := r.Context(), sessionOf(r).Profile.ID
	events, stop := a.svc.Events.Subscribe()
	defer stop()
	scans, err := a.svc.Events.Scans(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	hello := helloJSON{Scans: []scanJSON{}}
	for _, s := range scans {
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, s.Library)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if ok {
			hello.Scans = append(hello.Scans, scanJSON{LibraryID: s.Library, Phase: s.Phase, Done: s.Done, Known: s.Known})
		}
	}
	a.streamEvents(w, r, events, "hello", hello, func(e domain.Event) (eventJSON, bool, error) {
		return a.toldTo(ctx, profile, e)
	})
}

// toldTo answers an event as a profile is told it, and whether it is: its own state's changes,
// and libraries and titles it may see.
func (a *API) toldTo(ctx context.Context, profile uuid.UUID, e domain.Event) (eventJSON, bool, error) {
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
			e.Details = map[string]any{"title_ids": nonNil(same)}
		}
		return eventOf(e), true, nil
	case domain.EventTitleUpdated:
		seen, err := a.svc.Audience.Visible(ctx, profile, []uuid.UUID{e.Item})
		return eventOf(e), len(seen) == 1, err
	case domain.EventScanProgress, domain.EventLibraryScanned:
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, e.Library)
		return eventOf(e), ok, err
	case domain.EventLibraryChanged:
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, e.Library)
		if err != nil || !ok {
			return eventJSON{}, false, err
		}
		details, told := map[string]any{}, false
		for _, change := range domain.TitleChanges() {
			ids := idsIn(e.Details[string(change)])
			// A title removed is no longer there to ask of; its id says nothing of it.
			if change != domain.TitleRemoved {
				if ids, err = a.svc.Audience.Visible(ctx, profile, ids); err != nil {
					return eventJSON{}, false, err
				}
			}
			details[string(change)] = nonNil(ids)
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
		shown, _ := e.Details["playback"].(map[string]any)
		e.Details = map[string]any{"playback_id": shown["id"]}
		return eventOf(e), true, nil
	case domain.EventPlaybackStarted, domain.EventPlaybackPaused, domain.EventPlaybackResumed,
		domain.EventSignedIn, domain.EventSignInRefused,
		domain.EventProfileAdded, domain.EventProfileRemoved, domain.EventLibraryAdded,
		domain.EventLibraryRemoved, domain.EventTitlesAdded,
		domain.EventTaskStarted, domain.EventTaskFinished, domain.EventTaskFailed, domain.EventBackupMade,
		domain.EventJobStarted, domain.EventJobFinished, domain.EventJobFailed, domain.EventJobDead,
		domain.EventJobsProgress, domain.EventWebhookTest, domain.EventMaintenanceChanged,
		domain.EventNetworkChanged, domain.EventStorageChanged:
	}
	return eventJSON{}, false, nil
}

// idsIn reads the ids of a list in an event's details, which arrives from another node as JSON.
func idsIn(v any) []uuid.UUID {
	list, _ := v.([]any)
	var out []uuid.UUID
	for _, item := range list {
		s, _ := item.(string)
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}
