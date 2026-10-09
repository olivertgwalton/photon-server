//go:build integration

package remote

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func tool(t *testing.T, name, env string) string {
	t.Helper()
	path, err := exec.LookPath(cmp.Or(os.Getenv(env), name))
	if err != nil {
		t.Skipf("needs %s: %v", name, err)
	}
	return path
}

// film makes a film of 61 seconds, as long as minCopy and a second, served where the provider's
// host is.
func film(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	made := exec.CommandContext(t.Context(), tool(t, "ffmpeg", "PHOTON_FFMPEG"), "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x36:rate=5", "-t", "61", "-c:v", "libx264", "-preset", "ultrafast",
		filepath.Join(dir, "heat.mkv"))
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	if err := os.Link(filepath.Join(dir, "heat.mkv"), filepath.Join(dir, "heat-hd.mkv")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv
}

func open(t *testing.T) *store.Store {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

// unmatched matches nothing: the titles here carry the ids their streams are found by.
func unmatched(context.Context, uuid.UUID) error { return nil }

// offering offers what it is set to.
type offering struct{ offers []domain.Offer }

func (o *offering) Streams(context.Context, domain.FieldSource, domain.Streamed) ([]domain.Offer, error) {
	return o.offers, nil
}

// A remote film has no copy until it is opened: then its provider's best offer that can be read is
// read and kept as a version, opened where its provider offers it now, never at its address. A
// copy the provider stops offering is missing, and another it offers is read in its place.
func TestARemoteFilmIsGivenTheCopyItsProviderOffers(t *testing.T) {
	srv := film(t)
	st := open(t)
	ctx := t.Context()
	lib, err := st.AddRemoteLibrary(ctx, "Popular", domain.LibraryMovies, store.Remote{ListSource: aio, ListID: "movie/top", StreamSource: aio})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveListed(ctx, lib.ID, domain.ItemMovie, []domain.Listed{{Kind: domain.ItemMovie, IDs: heat.IDs, Title: "Heat"}}); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	titles, _, err := st.Search(ctx, store.SearchQuery{Text: "heat", Limit: 1})
	if err != nil || len(titles) != 1 {
		t.Fatalf("searching: %+v, %v", titles, err)
	}
	item = titles[0].ID
	at := func(path string) *url.URL {
		u, err := url.Parse(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	from := strings.TrimPrefix(srv.URL, "http://")
	dead := domain.Offer{Key: "torrent:dead:0", Name: "Heat.dead.mkv", URL: at("/gone.mkv"), From: from}
	uhd := domain.Offer{Key: "torrent:abc:0", Name: "Heat.2160p.mkv", URL: at("/heat.mkv"), From: from}
	provider := &offering{offers: []domain.Offer{dead, uhd}}
	tools := media.Tools{FFprobe: media.Tool{Path: tool(t, "ffprobe", "PHOTON_FFPROBE")}}
	copies := NewCopies(st, New(provider), tools, unmatched)

	if err := copies.Ensure(ctx, item); err != nil {
		t.Fatal(err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, item)
	if err != nil || len(page.Versions) != 1 || page.Versions[0].DurationMS < 60_000 {
		t.Fatalf("the film's copies %+v, %v; want the 2160p copy read, the dead link passed over", page.Versions, err)
	}
	c, err := st.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	parts := library.Parts{Places: st, Streams: New(provider)}
	in, err := parts.Open(ctx, c.Parts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(in.URL.String())
	if err != nil {
		t.Fatal(err)
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, 4))
	resp.Body.Close()
	in.Close()
	if err != nil || string(head) != "\x1a\x45\xdf\xa3" || strings.Contains(in.URL.String(), srv.URL) || in.Name != "Heat.2160p.mkv" {
		t.Errorf("opened %q at %s named %q, want the Matroska copy at the relay", head, in.URL, in.Name)
	}

	// Offered no more, the copy is missing and its part gone; another offered is read.
	hd := domain.Offer{Key: "torrent:abc:1", Name: "Heat.1080p.mkv", URL: at("/heat-hd.mkv"), From: from}
	provider.offers = []domain.Offer{hd}
	copies = NewCopies(st, New(provider), tools, unmatched)
	if err := copies.Ensure(ctx, item); err != nil {
		t.Fatal(err)
	}
	page, err = st.Title(ctx, uuid.UUID{}, item)
	if err != nil || len(page.Versions) != 2 {
		t.Fatalf("the film's copies %+v, %v; want both", page.Versions, err)
	}
	var missing int
	for _, v := range page.Versions {
		if v.MissingSince != nil {
			missing++
		}
	}
	if missing != 1 {
		t.Errorf("%d copies missing, want the 2160p one", missing)
	}
	if _, err := (library.Parts{Places: st, Streams: New(provider)}).Open(ctx, c.Parts[0].ID); !errors.Is(err, ErrGone) {
		t.Errorf("the 2160p copy opened: %v, want ErrGone", err)
	}

	provider.offers = nil
	if err := NewCopies(st, New(provider), tools, unmatched).Ensure(ctx, item); !errors.Is(err, ErrNoCopy) {
		t.Errorf("with nothing offered: %v, want ErrNoCopy", err)
	}
}

// A title a search found becomes its library's as it is opened, is matched there and then, and
// is given the copy its provider offers, as a title its list held is.
func TestAFoundTitleIsHeldMatchedAndGivenACopyAsItIsOpened(t *testing.T) {
	srv := film(t)
	st := open(t)
	ctx := t.Context()
	lib, err := st.AddRemoteLibrary(ctx, "Found", domain.LibraryMovies, store.Remote{DiscoverSource: domain.SourceTMDB, StreamSource: aio})
	if err != nil {
		t.Fatal(err)
	}
	found, err := st.SaveDiscoveries(ctx, lib.ID, domain.ItemMovie, domain.ProviderTMDB, []domain.Candidate{{ID: "949", Title: "Heat", Year: 1995}})
	if err != nil || len(found) != 1 {
		t.Fatalf("found %+v, %v", found, err)
	}
	u, err := url.Parse(srv.URL + "/heat.mkv")
	if err != nil {
		t.Fatal(err)
	}
	offers := &offering{offers: []domain.Offer{{Key: "torrent:abc:0", Name: "Heat.mkv", URL: u, From: strings.TrimPrefix(srv.URL, "http://")}}}
	var matched []uuid.UUID
	match := func(_ context.Context, id uuid.UUID) error {
		matched = append(matched, id)
		return nil
	}
	tools := media.Tools{FFprobe: media.Tool{Path: tool(t, "ffprobe", "PHOTON_FFPROBE")}}
	if err := NewCopies(st, New(offers), tools, match).Ensure(ctx, found[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, found[0].ID)
	if err != nil || page.Title != "Heat" || len(page.Versions) != 1 || len(matched) != 1 {
		t.Errorf("opened, it is %q with %d copies, matched %d times, %v", page.Title, len(page.Versions), len(matched), err)
	}
}

// A stream that does not answer in time is being fetched: the title says so, and the streams
// after it are left alone, so opening a title fetches one at most. One that fails is dead, and
// the next is read in its place.
func TestAStreamBeingFetchedIsWaitedForAndADeadOnePassedOver(t *testing.T) {
	srv := film(t)
	var asked atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)
	st := open(t)
	ctx := t.Context()
	lib, err := st.AddRemoteLibrary(ctx, "Popular", domain.LibraryMovies, store.Remote{ListSource: aio, ListID: "movie/top", StreamSource: aio})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveListed(ctx, lib.ID, domain.ItemMovie, []domain.Listed{{Kind: domain.ItemMovie, IDs: heat.IDs, Title: "Heat"}}); err != nil {
		t.Fatal(err)
	}
	titles, _, err := st.Search(ctx, store.SearchQuery{Text: "heat", Limit: 1})
	if err != nil || len(titles) != 1 {
		t.Fatalf("searching: %+v, %v", titles, err)
	}
	item := titles[0].ID
	at := func(base, path string) *url.URL {
		u, err := url.Parse(base + path)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	fetching := domain.Offer{Key: "release:Heat.Remux", Name: "Heat.Remux", URL: at(slow.URL, "/play?release=a"), From: strings.TrimPrefix(slow.URL, "http://")}
	from := strings.TrimPrefix(srv.URL, "http://")
	dead := domain.Offer{Key: "release:Heat.Dead", Name: "Heat.Dead", URL: at(srv.URL, "/gone.mkv"), From: from}
	ready := domain.Offer{Key: "release:Heat.WEB", Name: "Heat.WEB", URL: at(srv.URL, "/heat.mkv"), From: from}
	tools := media.Tools{FFprobe: media.Tool{Path: tool(t, "ffprobe", "PHOTON_FFPROBE")}}
	copies := func(offers ...domain.Offer) *Copies {
		c := NewCopies(st, New(&offering{offers: offers}), tools, unmatched)
		c.readyWithin = 200 * time.Millisecond
		return c
	}

	if err := copies(fetching, ready).Ensure(ctx, item); !errors.Is(err, ErrFetching) || asked.Load() != 1 {
		t.Errorf("first the one being fetched: %v, asked %d times; want ErrFetching, asked once", err, asked.Load())
	}
	if page, err := st.Title(ctx, uuid.UUID{}, item); err != nil || len(page.Versions) != 0 {
		t.Errorf("while it is fetched, %d copies, %v; want none, the ready one after it left alone", len(page.Versions), err)
	}
	if err := copies(dead, ready).Ensure(ctx, item); err != nil {
		t.Fatal(err)
	}
	if page, err := st.Title(ctx, uuid.UUID{}, item); err != nil || len(page.Versions) != 1 {
		t.Errorf("first a dead one, %d copies, %v; want the ready one after it", len(page.Versions), err)
	}
}
