//go:build integration

package subtitles

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// heat is a store with a films library holding Heat, one file of 128 KiB: the store, the
// library, and Heat's id.
func heat(t *testing.T) (*store.Store, domain.Library, uuid.UUID) {
	t.Helper()
	url, log := storetest.FreshDatabase(t), slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ctx, root := t.Context(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "H"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "H", "heat.mkv"), make([]byte, 128<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	film := store.Film{Title: "Heat", Folder: "H", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Copies: []store.Copy{{
		ContentKey: []byte("c"), Parts: []store.Part{{RelPath: "H/heat.mkv", Size: 128 << 10, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := st.SaveFolder(ctx, lib.ID, "H", []byte("v1"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	return st, lib, cards[0].ID
}

func TestANodeServesAFetchedSubtitleFromItsCache(t *testing.T) {
	st, _, item := heat(t)
	ctx := t.Context()
	c, err := st.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SaveFetchedSubtitle(ctx, c.Version, store.FetchedSubtitleFile{Body: []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n")})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files, err := NewFiles(st, dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		root, rel, err := files.SubtitleFile(ctx, id)
		if err != nil || root != dir {
			t.Fatalf("SubtitleFile = %q, %q, %v; want it in the cache", root, rel, err)
		}
		if body, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(body) != "1\n00:00:01,000 --> 00:00:02,000\nHi\n" {
			t.Fatalf("the file holds %q, %v", body, err)
		}
	}
}
