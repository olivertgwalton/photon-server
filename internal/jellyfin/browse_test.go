//go:build integration

package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// profiles signs in by token as the profile it names.
type profiles map[string]domain.Profile

func (profiles) SignIn(context.Context, string, string, auth.Device) (string, domain.Profile, error) {
	return "", domain.Profile{}, auth.ErrInvalidCredentials
}

func (p profiles) Authenticate(_ context.Context, token string) (domain.Session, error) {
	if pr, ok := p[token]; ok {
		return domain.Session{Kind: domain.SessionDevice, Profile: pr}, nil
	}
	return domain.Session{}, auth.ErrUnauthenticated
}

func (profiles) SignOut(context.Context, uuid.UUID) error { return nil }

func (profiles) StartPairing(context.Context, auth.Device, auth.CodeStyle) (auth.PairingStart, error) {
	return auth.PairingStart{}, errors.ErrUnsupported
}

func (profiles) ApprovePairing(context.Context, domain.Session, string) (auth.Device, error) {
	return auth.Device{}, auth.ErrPairingNotFound
}

func (profiles) PairingStatus(context.Context, string) (kv.PairingState, auth.Pairing, error) {
	return kv.PairingExpired, auth.Pairing{}, nil
}

func (profiles) PollPairing(context.Context, string) (kv.PairingState, string, domain.Profile, error) {
	return kv.PairingExpired, "", domain.Profile{}, nil
}

// Infuse's fields, which it sends on every list.
const infuseFields = "DateCreated,Etag,Genres,MediaSources,AlternateMediaSources,Overview,ParentId,Path,ProviderIds,SortName,RecursiveItemCount,ChildCount"

// An app browses a household's films and shows as Infuse does, from its libraries down to a show's
// episodes, and reads its rows; a profile sees only its own libraries.
func TestAnAppBrowsesTheLibraries(t *testing.T) {
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

	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	copies := func(rel string) []store.Copy {
		return []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1 << 30, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
			Duration: time.Hour, Container: "matroska,webm",
			Streams: []domain.Stream{
				{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 8, Compatibility: 1}},
				{Index: 1, Kind: domain.StreamAudio, Codec: "eac3", Language: language.English, Channels: 6, Default: true},
				{Index: 2, Kind: domain.StreamSubtitle, Codec: "subrip", Language: language.English},
			},
		}}}}}
	}
	for _, f := range []string{"Heat", "Alien"} {
		if _, err := st.SaveFolder(ctx, films.ID, f, []byte("v"), []store.Film{{Title: f, Folder: f, Copies: copies(f + "/" + f + ".mkv")}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var eps []store.Episode
	for _, se := range [][2]int{{1, 1}, {1, 2}, {2, 1}} {
		rel := "Wire/S" + string(rune('0'+se[0])) + "E" + string(rune('0'+se[1])) + ".mkv"
		eps = append(eps, store.Episode{Season: se[0], Episodes: []int{se[1]}, Title: rel, Folder: "Wire", ByNumber: true, Copies: copies(rel)})
	}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), store.Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	admin, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(ctx, "Kid", domain.RoleUser, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccess(ctx, kid.ID, store.ProfileAccess{Libraries: []uuid.UUID{films.ID}}); err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": admin, "pst_kid": kid}, Catalogue: st,
	})
	get := func(token, target string) any {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Infuse-Direct", Token="`+token+`"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		var v any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		return v
	}
	list := func(token, target string) ([]map[string]any, float64) {
		t.Helper()
		m, _ := get(token, target).(map[string]any)
		raw, _ := m["Items"].([]any)
		out := make([]map[string]any, len(raw))
		for n, it := range raw {
			out[n], _ = it.(map[string]any)
			requireKeys(t, target+" item", out[n], "Id", "Type", "Name", "ServerId", "ImageTags", "BackdropImageTags")
		}
		total, _ := m["TotalRecordCount"].(float64)
		return out, total
	}
	names := func(items []map[string]any) []string {
		var out []string
		for _, it := range items {
			out = append(out, it["Name"].(string))
		}
		return out
	}

	views, _ := list("pst_ada", "/UserViews?includeExternalContent=false&userId="+guid(admin.ID))
	if len(views) != 2 || views[0]["CollectionType"] != "movies" || views[1]["CollectionType"] != "tvshows" || views[0]["DisplayPreferencesId"] == nil {
		t.Fatalf("views: %v", views)
	}
	if kidViews, _ := list("pst_kid", "/UserViews"); len(kidViews) != 1 {
		t.Errorf("the kid sees %v, want the films alone", names(kidViews))
	}
	if folder, _ := get("pst_ada", "/Items/"+guid(films.ID)+"?fields="+infuseFields).(map[string]any); folder["Type"] != "CollectionFolder" {
		t.Errorf("a library by id: %v", folder)
	}

	page, total := list("pst_ada", "/Items?userId="+guid(admin.ID)+"&excludeLocationTypes=Virtual&parentId="+guid(films.ID)+
		"&sortBy=SortName&sortOrder=Ascending&includeItemTypes=Movie&recursive=true&startIndex=1&limit=1&fields="+infuseFields)
	if total != 2 || len(page) != 1 || page[0]["Name"] != "Heat" {
		t.Fatalf("the second film of two: %v of %v", names(page), total)
	}
	film := page[0]
	sources, _ := film["MediaSources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("a film's copies: %v", film["MediaSources"])
	}
	source, _ := sources[0].(map[string]any)
	requireKeys(t, "MediaSourceInfo", source, "Protocol", "Type", "IsRemote", "ReadAtNativeFramerate", "IgnoreDts", "IgnoreIndex",
		"GenPtsInput", "SupportsTranscoding", "SupportsDirectStream", "SupportsDirectPlay", "IsInfiniteStream",
		"RequiresOpening", "RequiresClosing", "RequiresLooping", "SupportsProbing", "TranscodingSubProtocol", "HasSegments")
	if source["Container"] != "mkv" || source["Path"] != "Heat.mkv" || film["Path"] != "Heat.mkv" {
		t.Errorf("container %v and file %v (%v), want an mkv named Heat.mkv: Infuse reads both", source["Container"], source["Path"], film["Path"])
	}
	streams, _ := source["MediaStreams"].([]any)
	for _, s := range streams {
		requireKeys(t, "MediaStream", s.(map[string]any), "IsInterlaced", "IsDefault", "IsForced", "IsHearingImpaired", "Type",
			"Index", "IsExternal", "IsTextSubtitleStream", "SupportsExternalStream")
	}
	if video, _ := streams[0].(map[string]any); video["VideoRangeType"] != "DOVIWithHDR10" {
		t.Errorf("a Dolby Vision profile 8 picture: %v", video)
	}
	userData, _ := film["UserData"].(map[string]any)
	requireKeys(t, "UserItemDataDto", userData, "PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "Key", "ItemId")

	all, _ := list("pst_ada", "/Items?recursive=true&includeItemTypes=Movie,Series&sortBy=SortName")
	if got := names(all); len(got) != 3 || got[0] != "Alien" || got[2] != "The Wire" {
		t.Errorf("every library at once: %v", got)
	}
	shows, _ := list("pst_ada", "/Items?parentId="+guid(tv.ID)+"&includeItemTypes=Series&recursive=true")
	show := shows[0]["Id"].(string)
	seasons, _ := list("pst_ada", "/Shows/"+show+"/Seasons?userId="+guid(admin.ID)+"&excludeLocationTypes=Virtual&fields=Genres,ParentId")
	if len(seasons) != 2 || seasons[0]["IndexNumber"] != 1.0 || seasons[0]["SeriesId"] != show {
		t.Errorf("seasons: %v", seasons)
	}
	episodes, total := list("pst_ada", "/Shows/"+show+"/Episodes?userId="+guid(admin.ID)+"&excludeLocationTypes=Virtual&fields=Etag,MediaSources,AlternateMediaSources,Genres,Overview,ParentId,ProviderIds")
	if total != 3 || episodes[2]["ParentIndexNumber"] != 2.0 || episodes[0]["SeriesId"] != show || episodes[0]["MediaSources"] == nil {
		t.Errorf("every episode of every season: %v", episodes)
	}
	if one, _ := list("pst_ada", "/Items?parentId="+seasons[1]["Id"].(string)); len(one) != 1 {
		t.Errorf("season 2's episodes: %v", names(one))
	}
	if w := serve(api, http.MethodGet, "/Shows/"+show+"/Episodes", `MediaBrowser Token="pst_kid"`, ""); w.Code != http.StatusNotFound {
		t.Errorf("a show the kid may not see: %d", w.Code)
	}

	first, _ := uuid.Parse(episodes[0]["Id"].(string))
	if err := st.MarkWatched(ctx, admin.ID, first, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProgress(ctx, admin.ID, uuidOf(t, film["Id"]), 10*time.Minute, time.Hour, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	if resume, _ := list("pst_ada", "/UserItems/Resume?limit=12&mediaTypes=Video&recursive=true&fields="+infuseFields); len(resume) != 1 || resume[0]["Name"] != "Heat" {
		t.Errorf("continue watching: %v", names(resume))
	}
	if next, _ := list("pst_ada", "/Shows/NextUp?limit=24&fields="+infuseFields); len(next) != 1 || next[0]["Id"] != episodes[1]["Id"] {
		t.Errorf("next up: %v", names(next))
	}
	latest, _ := get("pst_ada", "/Items/Latest?parentId="+guid(films.ID)+"&limit=20&fields="+infuseFields).([]any)
	if len(latest) != 2 {
		t.Errorf("latest films: %v", latest)
	}
	if trailers, _ := get("pst_ada", "/Items/"+show+"/LocalTrailers").([]any); trailers == nil || len(trailers) != 0 {
		t.Errorf("trailers: %v, want an empty list", trailers)
	}
}

func uuidOf(t *testing.T, v any) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(v.(string))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
