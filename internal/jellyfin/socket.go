package jellyfin

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
)

// lostAfter is how long an app's socket may say nothing before it is closed, as Jellyfin waits.
// An app is told it, and keeps the socket alive at half of it.
const lostAfter = 60 * time.Second

// messageType is what a message on the socket is, as Jellyfin's SessionMessageType names it.
type messageType string

const (
	messageForceKeepAlive  messageType = "ForceKeepAlive"
	messageKeepAlive       messageType = "KeepAlive"
	messageUserDataChanged messageType = "UserDataChanged"
	messageLibraryChanged  messageType = "LibraryChanged"
)

// message is Jellyfin's WebSocketMessage.
type message struct {
	MessageType messageType `json:"MessageType"`
	Data        any         `json:"Data,omitempty"`
	MessageID   string      `json:"MessageId"`
}

// userDataChanged is Jellyfin's UserDataChangeInfo.
type userDataChanged struct {
	UserID       string      `json:"UserId"`
	UserDataList []*userData `json:"UserDataList"`
}

// libraryUpdate is Jellyfin's LibraryUpdateInfo. Photon keeps a library's titles, not folders of
// them: the library is the folder each is added to or removed from.
type libraryUpdate struct {
	FoldersAddedTo     []string `json:"FoldersAddedTo"`
	FoldersRemovedFrom []string `json:"FoldersRemovedFrom"`
	ItemsAdded         []string `json:"ItemsAdded"`
	ItemsRemoved       []string `json:"ItemsRemoved"`
	ItemsUpdated       []string `json:"ItemsUpdated"`
	CollectionFolders  []string `json:"CollectionFolders"`
	IsEmpty            bool     `json:"IsEmpty"`
}

// audience says what of the libraries and titles a profile may be told of.
type audience interface {
	HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error)
	Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error)
	SameTitles(ctx context.Context, profile, title uuid.UUID) ([]uuid.UUID, error)
}

// stoppingKey holds, in a request's context, what closes as its listener stops serving: Shutdown
// neither waits for nor closes a socket, which the server stops tracking once it is hijacked.
type stoppingKey struct{}

// socket keeps a WebSocket open to the signed-in app, which keeps it alive, and tells it what
// changes of what the profile sees, on every node. What the app sends that is not a KeepAlive,
// such as SessionsStart, is let pass: none of it is served.
func (a *API) socket(w http.ResponseWriter, r *http.Request) {
	ctx, profile := r.Context(), auth.SessionOf(r.Context()).Profile.ID
	stopping, _ := ctx.Value(stoppingKey{}).(<-chan struct{})
	c, err := websocket.Accept(w, r)
	if err != nil {
		a.logger.DebugContext(ctx, "jellyfin socket not opened", slog.Any("err", err))
		return
	}
	defer a.closeSocket(ctx, c)
	events, unsubscribe := a.svc.Subscribe()
	defer unsubscribe()
	heard, done := make(chan messageType), make(chan struct{})
	defer close(done)
	go a.listen(ctx, c, heard, done)
	if !a.send(ctx, c, message{MessageType: messageForceKeepAlive, Data: int(lostAfter / time.Second)}) {
		return
	}
	for {
		select {
		case <-stopping:
			return
		case t, open := <-heard:
			if !open {
				return
			}
			if t == messageKeepAlive && !a.send(ctx, c, message{MessageType: messageKeepAlive}) {
				return
			}
		case e, open := <-events:
			if !open {
				return
			}
			m, told, err := a.told(ctx, profile, e)
			if err != nil {
				// The app opens it again, and reads again what it shows.
				a.logger.WarnContext(ctx, "jellyfin socket ended", slog.Any("err", err))
				return
			}
			if told && !a.send(ctx, c, m) {
				return
			}
		}
	}
}

// listen tells heard what kind of message the app sends, until it goes, breaks the protocol or
// says nothing for lostAfter, or done.
func (a *API) listen(ctx context.Context, c *websocket.Conn, heard chan<- messageType, done <-chan struct{}) {
	defer close(heard)
	for {
		m, err := c.Read(lostAfter)
		if err != nil {
			a.logger.DebugContext(ctx, "jellyfin socket ended", slog.Any("err", err))
			return
		}
		var in message
		if json.Unmarshal(m, &in) != nil {
			continue
		}
		select {
		case heard <- in.MessageType:
		case <-done:
			return
		}
	}
}

// told is the message an event is to a profile, and whether it is one: its own state of a title
// changed, and what changed of the libraries and titles it sees.
func (a *API) told(ctx context.Context, profile uuid.UUID, e domain.Event) (message, bool, error) {
	switch e.Kind {
	case domain.EventUserDataChanged:
		// A playlist changed is not told: no app is served one here.
		if e.Profile != profile || e.Item == (uuid.UUID{}) {
			return message{}, false, nil
		}
		return a.userDataChanged(ctx, profile, e.Item)
	case domain.EventLibraryChanged:
		ok, err := a.svc.Audience.HasLibrary(ctx, profile, e.Library)
		if err != nil || !ok {
			return message{}, false, err
		}
		changed, _ := e.Details.(domain.LibraryChangedDetails)
		return a.libraryChanged(ctx, profile, e.Library, changed)
	case domain.EventTitleUpdated:
		return a.libraryChanged(ctx, profile, uuid.UUID{}, domain.LibraryChangedDetails{domain.TitleUpdated: {e.Item}})
	case domain.EventPlaybackStarted, domain.EventPlaybackPaused, domain.EventPlaybackResumed,
		domain.EventPlaybackStopped, domain.EventSignedIn, domain.EventSignInRefused,
		domain.EventProfileAdded, domain.EventProfileRemoved, domain.EventLibraryAdded,
		domain.EventLibraryRemoved, domain.EventLibraryScanned, domain.EventTitlesAdded, domain.EventScanProgress,
		domain.EventTaskStarted, domain.EventTaskFinished, domain.EventTaskFailed, domain.EventBackupMade,
		domain.EventJobStarted, domain.EventJobFinished, domain.EventJobFailed, domain.EventJobDead,
		domain.EventJobsProgress, domain.EventWebhookTest, domain.EventMaintenanceChanged,
		domain.EventNetworkChanged, domain.EventStorageChanged, domain.EventNodesChanged, domain.EventRestoreStarted:
	}
	return message{}, false, nil
}

// userDataChanged is the profile's state of a title, which is its state wherever the title is
// listed, so each place is named.
func (a *API) userDataChanged(ctx context.Context, profile, title uuid.UUID) (message, bool, error) {
	same, err := a.svc.Audience.SameTitles(ctx, profile, title)
	if err != nil || len(same) == 0 {
		return message{}, false, err
	}
	p, err := a.svc.Catalogue.Title(ctx, profile, same[0])
	if isNotFound(err) {
		return message{}, false, nil
	}
	if err != nil {
		return message{}, false, err
	}
	var length int64
	if len(p.Versions) > 0 {
		length = p.Versions[0].DurationMS
	}
	list := make([]*userData, len(same))
	for n, id := range same {
		list[n] = a.userData(id, p.State, length, p.Kind)
	}
	return message{MessageType: messageUserDataChanged, Data: userDataChanged{UserID: guid(profile), UserDataList: list}}, true, nil
}

// libraryChanged is what changed of a library's titles that the profile sees, the library itself
// unnamed where it is not known.
func (a *API) libraryChanged(ctx context.Context, profile, lib uuid.UUID, changed domain.LibraryChangedDetails) (message, bool, error) {
	ids := map[domain.TitleChange][]string{domain.TitleAdded: {}, domain.TitleUpdated: {}, domain.TitleRemoved: {}}
	for change, titles := range changed {
		// A title removed is no longer there to ask of; its id says nothing of it.
		if change != domain.TitleRemoved {
			var err error
			if titles, err = a.svc.Audience.Visible(ctx, profile, titles); err != nil {
				return message{}, false, err
			}
		}
		for _, id := range titles {
			ids[change] = append(ids[change], guid(id))
		}
	}
	u := libraryUpdate{
		FoldersAddedTo: []string{}, FoldersRemovedFrom: []string{}, CollectionFolders: []string{},
		ItemsAdded: ids[domain.TitleAdded], ItemsRemoved: ids[domain.TitleRemoved], ItemsUpdated: ids[domain.TitleUpdated],
	}
	if len(u.ItemsAdded)+len(u.ItemsRemoved)+len(u.ItemsUpdated) == 0 {
		return message{}, false, nil
	}
	if lib != (uuid.UUID{}) {
		u.CollectionFolders = []string{guid(lib)}
		if len(u.ItemsAdded) > 0 {
			u.FoldersAddedTo = u.CollectionFolders
		}
		if len(u.ItemsRemoved) > 0 {
			u.FoldersRemovedFrom = u.CollectionFolders
		}
	}
	return message{MessageType: messageLibraryChanged, Data: u}, true, nil
}

func (a *API) closeSocket(ctx context.Context, c *websocket.Conn) {
	if err := c.Close(websocket.StatusGoingAway); err != nil {
		a.logger.DebugContext(ctx, "jellyfin socket not closed cleanly", slog.Any("err", err))
	}
}

// send writes a message under an id of its own, as Jellyfin writes every one.
func (a *API) send(ctx context.Context, c *websocket.Conn, m message) bool {
	m.MessageID = guid(uuid.NewV7())
	body, err := json.Marshal(m)
	if err != nil {
		a.logger.ErrorContext(ctx, "jellyfin message not encoded", slog.Any("err", err))
		return false
	}
	if err := c.Write(body); err != nil {
		a.logger.DebugContext(ctx, "jellyfin message not sent", slog.Any("err", err))
		return false
	}
	return true
}
