//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAFetchedSubtitleOutlivesAScanAndIsServedFromPostgres(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "H", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Copies: []Copy{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "H/heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "H", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	search, err := s.SubtitleSearchOf(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil || search.Query.Kind != domain.ItemMovie || search.Query.IDs[domain.ProviderIMDb] != "tt0113277" ||
		search.Part == (uuid.UUID{}) || search.Parts != 1 {
		t.Fatalf("SubtitleSearchOf = %+v, %v; want the film's IMDb id and its one file", search, err)
	}
	id, err := s.SaveFetchedSubtitle(ctx, search.Version, FetchedSubtitleFile{Language: language.French, Body: []byte("1\n")})
	if err != nil {
		t.Fatal(err)
	}
	// The scan finds no such file in the library, and keeps it all the same.
	if _, err := s.FinishScan(ctx, lib.ID, []string{"."}, []string{"H"}, []string{"H/heat.mkv"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil || len(c.Subtitles) != 1 || c.Subtitles[0].ID != id || c.Subtitles[0].Language != language.French {
		t.Fatalf("after a scan, the copy's subtitles: %+v, %v; want the fetched French one", c.Subtitles, err)
	}
	// It is in no library: its text is in Postgres.
	if root, _, err := s.SubtitleFile(ctx, id); err != nil || root != "" {
		t.Errorf("SubtitleFile = %q, %v; want no library root", root, err)
	}
	if body, err := s.SubtitleBody(ctx, id); err != nil || string(body) != "1\n" {
		t.Errorf("SubtitleBody = %q, %v", body, err)
	}
	if err := s.RemoveFetchedSubtitle(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFetchedSubtitle(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing it again: %v, want ErrNotFound", err)
	}
}

func TestACopyWantsASubtitleInEachLanguageItsLibraryNamesThatItLacks(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	film := func(lib uuid.UUID, title string, streams []domain.Stream, files []Subtitle) uuid.UUID {
		t.Helper()
		f := Film{Title: title, Folder: title, Copies: []Copy{{ContentKey: []byte(title), Subtitles: files, Parts: []Part{{
			RelPath: title + "/" + title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour, Streams: streams},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib, title, []byte("v1"), []Film{f}, nil); err != nil {
			t.Fatal(err)
		}
		var version uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT v.id FROM versions v JOIN items i ON i.id = v.item_id WHERE i.title = $1`, title).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return version
	}
	// Heat has English inside it, in the English of Britain; Ronin French forced alone beside it,
	// which is no subtitle for the whole film.
	heat := film(lib.ID, "Heat", []domain.Stream{{Index: 2, Kind: domain.StreamSubtitle, Codec: "subrip", Language: language.BritishEnglish}}, nil)
	ronin := film(lib.ID, "Ronin", nil, []Subtitle{{RelPath: "Ronin/Ronin.fr.forced.srt", Size: 1, ModTime: time.Unix(0, 0), Codec: "subrip", Language: language.French, Forced: true}})
	film(other.ID, "Tenet", nil, nil)
	err = s.SetLibrary(ctx, lib.ID, LibraryChange{SubtitleLanguages: []language.Tag{language.English, language.French}, SubtitleMatch: domain.SubtitleMatchAny})
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		version uuid.UUID
		lang    language.Tag
	}
	all := func(since time.Time) []want {
		t.Helper()
		var out []want
		// A page at a time, as a run reads them.
		var after *WantedSubtitle
		for {
			page, err := s.WantedSubtitles(ctx, since, after, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				return out
			}
			if page[0].Match != domain.SubtitleMatchAny {
				t.Errorf("%+v: want the library's match", page[0])
			}
			out = append(out, want{page[0].Version, page[0].Language})
			after = &page[0]
		}
	}
	// Newest first: Ronin was found after Heat.
	if got, w := all(time.Now()), []want{{ronin, language.French}, {ronin, language.English}, {heat, language.French}}; !slices.Equal(got, w) {
		t.Fatalf("wanted %v, want %v", got, w)
	}
	if err := s.SubtitleSearched(ctx, ronin, language.French); err != nil {
		t.Fatal(err)
	}
	// Searched for now, it is wanted again only once its search is old enough.
	if got := all(time.Now().Add(-time.Hour)); len(got) != 2 || slices.Contains(got, want{ronin, language.French}) {
		t.Errorf("after searching Ronin in French: %v", got)
	}
	if got := all(time.Now().Add(time.Hour)); len(got) != 3 {
		t.Errorf("once that search is old: %v, want all three again", got)
	}
}
