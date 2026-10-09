package jellyfin

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

func openSocket(t *testing.T, target string, header http.Header) *websockettest.Client {
	t.Helper()
	c, resp, err := websockettest.Dial(t.Context(), target, header)
	if err != nil || c == nil {
		t.Fatalf("%s: %v %v", target, resp, err)
	}
	resp.Body.Close()
	t.Cleanup(func() { c.Close() })
	return c
}

func say(t *testing.T, c *websockettest.Client, m string) {
	t.Helper()
	if err := c.Send(websockettest.Fin|websockettest.OpText, []byte(m)); err != nil {
		t.Fatal(err)
	}
}

// heard reads the server's next message, failing on any frame that is not one.
func heard(t *testing.T, c *websockettest.Client) map[string]any {
	t.Helper()
	head, payload, err := c.Next()
	if err != nil || head != websockettest.Fin|websockettest.OpText {
		t.Fatalf("not a message: %#x %q %v", head, payload, err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	if id, _ := m["MessageId"].(string); !hexID.MatchString(id) {
		t.Errorf("MessageId %q, want a Guid as Jellyfin writes one", id)
	}
	return m
}

// An app opens its socket with its token in the query, as Jellyfin's web app does, or its header;
// it is told how often to keep it alive, and each KeepAlive is answered, what it sends besides let
// pass. One without a token is refused before any socket is opened.
func TestAnAppKeepsItsSocketAlive(t *testing.T) {
	api, _, _, _ := newAPI()
	srv := httptest.NewServer(api)
	defer srv.Close()
	for _, open := range []struct {
		target string
		header http.Header
	}{
		{srv.URL + "/socket?ApiKey=pst_device&deviceId=TW96", nil},
		{srv.URL + "/Socket", http.Header{"Authorization": {kotlin + `, Token="pst_device"`}}},
	} {
		c := openSocket(t, open.target, open.header)
		if m := heard(t, c); m["MessageType"] != "ForceKeepAlive" || m["Data"] != float64(60) {
			t.Errorf("%s: first %v, want ForceKeepAlive every 60 seconds", open.target, m)
		}
		say(t, c, `{"MessageType":"SessionsStart","Data":"0,1500"}`)
		say(t, c, `not even JSON`)
		say(t, c, `{"MessageType":"KeepAlive"}`)
		if m := heard(t, c); m["MessageType"] != "KeepAlive" {
			t.Errorf("%s: a KeepAlive answered with %v", open.target, m)
		}
	}
	_, resp, err := websockettest.Dial(t.Context(), srv.URL+"/socket?ApiKey=pst_stranger", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unknown token: %d, want 401", resp.StatusCode)
	}
}

// pipedResponse is a response whose connection is one end of a pipe, which synctest's clock times
// as it times no socket.
type pipedResponse struct {
	http.ResponseWriter
	conn net.Conn
	r    *bufio.Reader
}

func (p pipedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return p.conn, bufio.NewReadWriter(p.r, bufio.NewWriter(p.conn)), nil
}

// An app's socket stays open while it keeps it alive, and is closed once the app has said nothing
// for a minute.
func TestASilentAppsSocketIsClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, _, _, _ := newAPI()
		server, client := net.Pipe()
		served := make(chan struct{})
		go func() {
			defer close(served)
			br := bufio.NewReader(server)
			r, err := http.ReadRequest(br)
			if err != nil {
				t.Error(err)
				return
			}
			api.ServeHTTP(pipedResponse{httptest.NewRecorder(), server, br}, r)
		}()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/socket?ApiKey=pst_device", nil)
		c, resp, err := websockettest.Open(client, r)
		if err != nil || c == nil {
			t.Fatalf("%v %v", resp, err)
		}
		resp.Body.Close()
		heard(t, c)
		start := time.Now()
		<-time.After(45 * time.Second)
		say(t, c, `{"MessageType":"KeepAlive"}`)
		heard(t, c)
		head, _, err := c.Next()
		if err != nil || head != websockettest.Fin|websockettest.OpClose {
			t.Fatalf("after the silence: %#x %v, want a close", head, err)
		}
		if waited := time.Since(start); waited != 45*time.Second+lostAfter {
			t.Errorf("closed after %v, want a minute after the last KeepAlive", waited)
		}
		<-served
	})
}

// shelf is a household's titles as one profile, Ada, may see them: one library of hers, a title
// she may not see, a film listed twice, and a playlist of hers.
type shelf struct {
	catalogue
	library, hidden, film, twin, playlist uuid.UUID
}

func (s shelf) Named(_ context.Context, profile, id uuid.UUID) (store.Named, error) {
	if profile == ada.ID && id == s.playlist {
		return store.Named{Kind: store.NamedPlaylist}, nil
	}
	return store.Named{}, store.ErrNotFound
}

func (s shelf) HasLibrary(_ context.Context, profile, lib uuid.UUID) (bool, error) {
	return profile == ada.ID && lib == s.library, nil
}

func (s shelf) Visible(_ context.Context, _ uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error) {
	return slices.DeleteFunc(slices.Clone(titles), func(id uuid.UUID) bool { return id == s.hidden }), nil
}

func (s shelf) SameTitles(_ context.Context, _, title uuid.UUID) ([]uuid.UUID, error) {
	if title == s.film {
		return []uuid.UUID{s.film, s.twin}, nil
	}
	return []uuid.UUID{title}, nil
}

func (s shelf) Title(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	watched := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	return store.TitlePage{
		ID: id, Kind: domain.ItemMovie, Versions: []store.VersionPage{{DurationMS: 7_200_000}},
		State: store.TitleState{PositionMS: 1_800_000, Plays: 1, WatchedAt: &watched, LastPlayedAt: &watched, FavouriteAt: &watched},
	}, nil
}

// An app's socket is told what changes of what its profile sees, in Jellyfin's words: its own
// state of a title, wherever the title is listed, and the titles of its libraries added, changed
// and removed. Another profile's state, a library it does not have and a title it may not see are
// never told.
func TestAnAppIsToldWhatChangesOfWhatItsProfileSees(t *testing.T) {
	s := shelf{library: uuid.NewV7(), hidden: uuid.NewV7(), film: uuid.NewV7(), twin: uuid.NewV7(), playlist: uuid.NewV7()}
	stranger, added, removed, deleted := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	events := make(chan domain.Event, 16)
	api := New(slog.New(slog.DiscardHandler), serverID.String(), func() string { return "Den" }, Services{
		Copies: noCopies{},
		Auth:   fakeAuth{}, Catalogue: s, Audience: s,
		Subscribe: func() (<-chan domain.Event, func()) { return events, func() {} },
	})
	srv := httptest.NewServer(api)
	defer srv.Close()
	c := openSocket(t, srv.URL+"/socket?ApiKey=pst_device", nil)
	heard(t, c)
	stopped := func(device uuid.UUID, by domain.StoppedBy) domain.Event {
		return domain.Event{Kind: domain.EventPlaybackStopped, Profile: ada.ID, Details: domain.PlaybackDetails{
			Playback: domain.NowPlaying{PlaybackCard: domain.PlaybackCard{Device: domain.PlaybackDevice{ID: device}}}, StoppedBy: by,
		}}
	}
	thisDevice := uuid.MustParse("00000000-0000-0000-0000-00000000000d")

	for _, e := range []domain.Event{
		{Kind: domain.EventUserDataChanged, Profile: stranger, Item: s.film},
		{Kind: domain.EventUserDataChanged, Profile: stranger, Details: domain.UserDataDetails{PlaylistID: s.playlist}},
		{Kind: domain.EventLibraryChanged, Library: uuid.NewV7(), Details: domain.LibraryChangedDetails{domain.TitleAdded: {added}}},
		{Kind: domain.EventLibraryChanged, Library: s.library, Details: domain.LibraryChangedDetails{domain.TitleAdded: {s.hidden}}},
		{Kind: domain.EventTitleUpdated, Item: s.hidden},
		stopped(thisDevice, domain.StoppedByPlayer),
		stopped(uuid.NewV7(), domain.StoppedByAdmin),
		{Kind: domain.EventUserDataChanged, Profile: ada.ID, Item: s.film},
		{Kind: domain.EventLibraryChanged, Library: s.library, Details: domain.LibraryChangedDetails{
			domain.TitleAdded: {added, s.hidden}, domain.TitleRemoved: {removed}, domain.TitleUpdated: {},
		}},
		{Kind: domain.EventTitleUpdated, Item: s.film},
		{Kind: domain.EventUserDataChanged, Profile: ada.ID, Details: domain.UserDataDetails{PlaylistID: s.playlist}},
		{Kind: domain.EventUserDataChanged, Profile: ada.ID, Details: domain.UserDataDetails{PlaylistID: deleted}},
		stopped(thisDevice, domain.StoppedByAdmin),
	} {
		events <- e
	}

	m := heard(t, c)
	data, _ := m["Data"].(map[string]any)
	list, _ := data["UserDataList"].([]any)
	if m["MessageType"] != "UserDataChanged" || data["UserId"] != guid(ada.ID) || len(list) != 2 {
		t.Fatalf("first told %v, want Ada's state of the film, and nothing of what she may not see", m)
	}
	for n, id := range []uuid.UUID{s.film, s.twin} {
		u, _ := list[n].(map[string]any)
		requireKeys(t, "UserItemDataDto", u, "PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "Key", "ItemId")
		if u["ItemId"] != guid(id) || u["Played"] != true || u["IsFavorite"] != true ||
			u["PlaybackPositionTicks"] != float64(18_000_000_000) || u["PlayedPercentage"] != float64(25) {
			t.Errorf("UserData %d: %v", n, u)
		}
	}

	m = heard(t, c)
	data, _ = m["Data"].(map[string]any)
	lib := []any{guid(s.library)}
	if m["MessageType"] != "LibraryChanged" || !reflect.DeepEqual(data, map[string]any{
		"ItemsAdded": []any{guid(added)}, "ItemsRemoved": []any{guid(removed)}, "ItemsUpdated": []any{},
		"FoldersAddedTo": lib, "FoldersRemovedFrom": lib, "CollectionFolders": lib, "IsEmpty": false,
	}) {
		t.Errorf("a library's titles changed: %v", m)
	}
	m = heard(t, c)
	data, _ = m["Data"].(map[string]any)
	if m["MessageType"] != "LibraryChanged" || !reflect.DeepEqual(data["ItemsUpdated"], []any{guid(s.film)}) {
		t.Errorf("a title described again: %v", m)
	}
	for _, want := range []struct {
		field string
		id    uuid.UUID
	}{{"ItemsUpdated", s.playlist}, {"ItemsRemoved", deleted}} {
		m = heard(t, c)
		data, _ = m["Data"].(map[string]any)
		if m["MessageType"] != "LibraryChanged" || !reflect.DeepEqual(data[want.field], []any{guid(want.id)}) {
			t.Errorf("her playlist changed, want it in %s: %v", want.field, m)
		}
	}
	// Only the stop an admin made of this device's own playback closes its player.
	m = heard(t, c)
	if m["MessageType"] != "Playstate" || !reflect.DeepEqual(m["Data"], map[string]any{"Command": "Stop"}) {
		t.Errorf("an admin stopped its playback: %v", m)
	}
}
