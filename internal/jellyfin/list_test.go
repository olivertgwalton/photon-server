package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// longLibrary is a film library longer than a list's chunk: every third film has a copy, every
// third more is streamed, and every film has an IMDb id. Its copies fail to be read after
// failAfter reads, where that is set.
type longLibrary struct {
	catalogue
	lib       uuid.UUID
	cards     []store.Card
	copies    map[uuid.UUID]uuid.UUID
	reads     *int
	failAfter int
}

func newLongLibrary(n int) longLibrary {
	l := longLibrary{lib: uuid.NewV7(), copies: map[uuid.UUID]uuid.UUID{}, reads: new(int)}
	for i := range n {
		l.cards = append(l.cards, store.Card{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Film " + strconv.Itoa(i), Year: 1900 + i%120})
		if i%3 == 0 {
			l.copies[l.cards[i].ID] = uuid.NewV7()
		}
	}
	return l
}

func (l longLibrary) LibrariesSeen(context.Context, uuid.UUID) ([]*store.SeenLibrary, error) {
	return []*store.SeenLibrary{{ID: l.lib, Name: "Films", Kind: domain.LibraryMovies}}, nil
}

func (l longLibrary) Wall(_ context.Context, _ []uuid.UUID, p store.WallPage) ([]uuid.UUID, store.Extent, error) {
	var ids []uuid.UUID
	for _, c := range l.cards[min(p.Offset, len(l.cards)):min(p.Offset+p.Limit, len(l.cards))] {
		ids = append(ids, c.ID)
	}
	if p.Count != domain.CountExact {
		return nil, store.Extent{}, errors.New("a Jellyfin list is counted")
	}
	return ids, store.Extent{More: p.Offset+len(ids) < len(l.cards), Total: new(int64(len(l.cards)))}, nil
}

func (l longLibrary) Cards(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]store.Card, error) {
	out := make([]store.Card, len(ids))
	for n, id := range ids {
		out[n] = l.cards[l.nth(id)]
	}
	return out, nil
}

func (l longLibrary) nth(id uuid.UUID) int {
	for n, c := range l.cards {
		if c.ID == id {
			return n
		}
	}
	return -1
}

func (l longLibrary) Versions(_ context.Context, items []uuid.UUID) (map[uuid.UUID][]store.VersionPage, error) {
	if *l.reads++; l.failAfter > 0 && *l.reads > l.failAfter {
		return nil, errors.New("database gone")
	}
	out := map[uuid.UUID][]store.VersionPage{}
	for _, id := range items {
		if v, ok := l.copies[id]; ok {
			out[id] = []store.VersionPage{{ID: v, Container: "matroska,webm", DurationMS: 7_200_000, SizeBytes: 1 << 30, Parts: 1, Streams: []domain.Stream{
				{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeSDR},
			}}}
		}
	}
	return out, nil
}

func (l longLibrary) Streamed(_ context.Context, items []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range items {
		out[id] = l.nth(id)%3 == 1
	}
	return out, nil
}

func (l longLibrary) ExternalIDs(_ context.Context, items []uuid.UUID) (map[uuid.UUID]map[domain.Provider]string, error) {
	out := map[uuid.UUID]map[domain.Provider]string{}
	for _, id := range items {
		out[id] = map[domain.Provider]string{domain.ProviderIMDb: "tt" + strconv.Itoa(l.nth(id))}
	}
	return out, nil
}

func longAPI(l longLibrary) *API {
	return New(slog.New(slog.DiscardHandler), serverID.String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{}, Auth: fakeAuth{}, Catalogue: l, Preferences: newPreferences{},
	})
}

// A list longer than a chunk is answered as one reply encoded whole: every item in order, with its
// copies, placeholder and ids where asked, and the list's count and start.
func TestAnAppIsAnsweredALongListWhole(t *testing.T) {
	ctx := t.Context()
	l := newLongLibrary(2*listChunk + 7)
	api := longAPI(l)
	for _, target := range []string{
		"/Items?Recursive=true&IncludeItemTypes=Movie",
		"/Items?Recursive=true&IncludeItemTypes=Movie&StartIndex=3&Fields=DateCreated,Etag,Genres,MediaSources,Overview,ParentId,Path,ProviderIds,SortName",
		"/Items?Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Chapters,Trickplay",
		"/Items?Recursive=true&IncludeItemTypes=Movie&StartIndex=" + strconv.Itoa(len(l.cards)),
	} {
		w := serve(api, http.MethodGet, target, `MediaBrowser Token="pst_device"`, "")
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s: %d %s", target, w.Code, w.Header().Get("Content-Type"))
		}
		r := httptest.NewRequest(http.MethodGet, target, nil)
		asked := listedOf(httptest.NewRecorder(), r)
		page := l.cards[min(asked.start, len(l.cards)):]
		items, err := api.list(ctx, page, asked)
		if err != nil {
			t.Fatal(err)
		}
		whole, err := json.Marshal(queryResult{Items: items, TotalRecordCount: len(l.cards), StartIndex: asked.start})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := w.Body.String(), string(whole)+"\n"; got != want {
			t.Errorf("%s: answered\n%.300s\nwant\n%.300s", target, got, want)
		}
		var got queryResult
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if len(got.Items) != len(page) || len(page) > 0 && got.Items[len(page)-1].Name != page[len(page)-1].Title {
			t.Errorf("%s: %d items", target, len(got.Items))
		}
	}
}

// A list that fails partway through its reply breaks the connection, so the app cannot take what
// it was sent for the whole list; one that fails before it begins is answered 500.
func TestAnAppIsNotAnsweredPartOfAList(t *testing.T) {
	l := newLongLibrary(2*listChunk + 7)
	l.failAfter = 1
	srv := httptest.NewServer(longAPI(l))
	t.Cleanup(srv.Close)
	get := func() (*http.Response, error) {
		r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/Items?Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", `MediaBrowser Token="pst_device"`)
		return srv.Client().Do(r)
	}
	res, err := get()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(res.Body); err == nil {
		t.Error("a list that failed partway was read whole")
	}
	res.Body.Close()

	*l.reads = 1
	res, err = get()
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("a list that failed at once: %d", res.StatusCode)
	}
}
