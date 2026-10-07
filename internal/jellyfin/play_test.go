//go:build integration

package jellyfin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// fakePlaybacks is the dashboard's playbacks: the one it started, and what it was told of it.
type fakePlaybacks struct {
	id       uuid.UUID
	started  []domain.PlaybackCard
	told     []time.Duration
	stoppedA time.Duration
}

func (f *fakePlaybacks) Start(_ context.Context, _ uuid.UUID, _ domain.PlayMethod, card domain.PlaybackCard) (domain.Playback, error) {
	f.started = append(f.started, card)
	return domain.Playback{ID: f.id}, nil
}

func (f *fakePlaybacks) Progress(_ context.Context, _, id uuid.UUID, position time.Duration, _ domain.PlayState, _ domain.ChosenTracks) (domain.Reach, error) {
	if id != f.id {
		return "", playback.ErrNoPlayback
	}
	f.told = append(f.told, position)
	return domain.ReachResumable, nil
}

func (f *fakePlaybacks) Stop(_ context.Context, _, id uuid.UUID, position time.Duration) (domain.Reach, error) {
	if id != f.id {
		return "", playback.ErrNoPlayback
	}
	f.stoppedA = position
	return domain.ReachResumable, nil
}

// An app plays a film as Infuse does: asks for its copies, which starts the playback the dashboard
// shows, streams the file with ranges, skips its intro, reads its subtitles beside it, and says
// where it got to; and marks it watched and a favourite.
func TestAnAppPlaysAFilm(t *testing.T) {
	ctx := t.Context()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Heat"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"Heat/Heat.mkv": "0123456789", "Heat/Heat.en.srt": "1\n00:00:01,000 --> 00:00:02,000\nHello\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	part := store.Part{RelPath: "Heat/Heat.mkv", Size: 10, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Duration: time.Hour, Container: "matroska,webm",
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR},
			{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 2, Language: language.English},
		},
	}}
	film := store.Film{Title: "Heat", Folder: "Heat", Copies: []store.Copy{{ContentKey: []byte("heat"), Parts: []store.Part{part}, Subtitles: []store.Subtitle{
		{RelPath: "Heat/Heat.en.srt", Size: 1, ModTime: time.Unix(0, 0), Codec: "subrip", Language: language.English},
	}}}}
	if _, err := st.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	heat := cards[0].ID
	versions, err := st.Versions(ctx, []uuid.UUID{heat})
	if err != nil {
		t.Fatal(err)
	}
	copyID := versions[heat][0].ID
	if err := st.SetMarkers(ctx, copyID, []domain.Marker{{Kind: domain.MarkerIntro, StartMS: 1000, EndMS: 61000}}, nil); err != nil {
		t.Fatal(err)
	}

	plays := &fakePlaybacks{id: uuid.NewV7()}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Playbacks: plays, Watching: st,
	})
	const infuse = `MediaBrowser Client="Infuse-Direct", Device="Apple TV", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	call := func(method, target, body string, want int) []byte {
		t.Helper()
		w := serve(api, method, target, infuse, body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, target, w.Code, w.Body, want)
		}
		return w.Body.Bytes()
	}

	var info struct {
		MediaSources  []map[string]any
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(call(http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo",
		`{"UserId":"`+guid(ada.ID)+`","IsPlayback":true,"AutoOpenLiveStream":true,"EnableDirectPlay":true,"MaxStreamingBitrate":200000000,"DirectPlayProtocols":["Http"],"DeviceProfile":{"MaxStaticBitrate":200000000}}`,
		http.StatusOK), &info); err != nil {
		t.Fatal(err)
	}
	if info.PlaySessionID != guid(plays.id) || len(plays.started) != 1 || plays.started[0].Profile.ID != ada.ID {
		t.Fatalf("PlaybackInfo: session %s, started %+v", info.PlaySessionID, plays.started)
	}
	if len(info.MediaSources) != 1 || info.MediaSources[0]["Id"] != guid(copyID) || info.MediaSources[0]["SupportsDirectPlay"] != true {
		t.Errorf("sources: %v", info.MediaSources)
	}
	streams, _ := info.MediaSources[0]["MediaStreams"].([]any)
	external, _ := streams[len(streams)-1].(map[string]any)
	if external["IsExternal"] != true || external["Index"] != 2.0 || external["Language"] != "eng" {
		t.Errorf("the subtitle file beside it: %v", external)
	}

	w := serve(api, http.MethodGet, "/Videos/"+guid(heat)+"/stream?MediaSourceId="+guid(copyID)+"&Static=true", infuse, "")
	if w.Code != http.StatusOK || w.Body.String() != "0123456789" {
		t.Errorf("the stream: %d %q", w.Code, w.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/videos/"+guid(heat)+"/stream.mkv?static=true", nil)
	r.Header.Set("Authorization", infuse)
	r.Header.Set("Range", "bytes=2-4")
	rw := httptest.NewRecorder()
	api.ServeHTTP(rw, r)
	if rw.Code != http.StatusPartialContent || rw.Body.String() != "234" {
		t.Errorf("a range of the stream: %d %q", rw.Code, rw.Body)
	}
	if sub := call(http.MethodGet, "/Videos/"+guid(heat)+"/"+guid(copyID)+"/Subtitles/2/0/Stream.srt", "", http.StatusOK); !strings.Contains(string(sub), "Hello") {
		t.Errorf("the subtitle file: %q", sub)
	}
	call(http.MethodGet, "/Videos/"+guid(heat)+"/"+guid(copyID)+"/Subtitles/9/0/Stream.srt", "", http.StatusNotFound)

	var segs struct{ Items []map[string]any }
	if err := json.Unmarshal(call(http.MethodGet, "/MediaSegments/"+guid(heat), "", http.StatusOK), &segs); err != nil {
		t.Fatal(err)
	}
	if len(segs.Items) != 1 || segs.Items[0]["Type"] != "Intro" || segs.Items[0]["StartTicks"] != 1e7 || segs.Items[0]["EndTicks"] != 61e7 {
		t.Errorf("segments: %v", segs.Items)
	}

	report := func(path string, ticks int64, session string) {
		call(http.MethodPost, path, `{"ItemId":"`+guid(heat)+`","PlaySessionId":"`+session+`","PositionTicks":`+
			jsonInt(ticks)+`,"PlayMethod":"DirectStream","IsPaused":false,"AudioStreamIndex":1,"SubtitleStreamIndex":-1}`, http.StatusNoContent)
	}
	report("/Sessions/Playing", 0, info.PlaySessionID)
	report("/Sessions/Playing/Progress", 10*60*1e7, info.PlaySessionID)
	report("/Sessions/Playing/Stopped", 12*60*1e7, info.PlaySessionID)
	if len(plays.told) != 2 || plays.told[1] != 10*time.Minute || plays.stoppedA != 12*time.Minute {
		t.Errorf("the playback was told %v and stopped at %v", plays.told, plays.stoppedA)
	}
	// An app that played without asking first is still kept to where it got to.
	report("/Sessions/Playing/Stopped", 20*60*1e7, "aa11bb22cc33dd44ee55ff6677889900")
	page, err := st.Title(ctx, ada.ID, heat)
	if err != nil || page.State.PositionMS != (20*time.Minute).Milliseconds() {
		t.Errorf("progress kept without a playback: %+v, %v", page.State, err)
	}

	var data map[string]any
	if err := json.Unmarshal(call(http.MethodPost, "/UserPlayedItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["Played"] != true {
		t.Errorf("marked watched: %v, %v", data, err)
	}
	if err := json.Unmarshal(call(http.MethodPost, "/Users/"+guid(ada.ID)+"/FavoriteItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["IsFavorite"] != true {
		t.Errorf("a favourite: %v, %v", data, err)
	}
	if err := json.Unmarshal(call(http.MethodDelete, "/UserFavoriteItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["IsFavorite"] != false {
		t.Errorf("no longer a favourite: %v, %v", data, err)
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
