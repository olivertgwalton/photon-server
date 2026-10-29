//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestEachPlayIsKeptInTheHistory(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "Heat", Versions: []Version{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	c, err := s.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	for n, who := range []uuid.UUID{oliver.ID, kid.ID, oliver.ID} {
		p := domain.Playback{ID: uuid.NewV7(), Profile: who, Item: item, Version: c.Version, Method: domain.PlayRemux, Started: start.Add(time.Duration(n) * time.Minute)}
		if err := s.RecordPlay(ctx, p, p.Started.Add(10*time.Minute), time.Duration(n+1)*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	mine, more, err := s.History(ctx, oliver.ID, Newest, 10)
	if err != nil || more || len(mine) != 2 || mine[0].PositionMS != 3*60_000 || mine[0].Card.Title != "Heat" || mine[0].Method != domain.PlayRemux {
		t.Errorf("Oliver's = %+v, more %v, %v; want his two, the latest first, and no more", mine, more, err)
	}
	first, more, err := s.History(ctx, uuid.UUID{}, Newest, 1)
	if err != nil || !more || len(first) != 1 || first[0].Profile != oliver.ID {
		t.Fatalf("everyone's first = %+v, more %v, %v; want Oliver's latest, and more", first, more, err)
	}
	// A play stopped as the history is read is on no later page: the next goes on from the last read.
	p := domain.Playback{ID: uuid.NewV7(), Profile: kid.ID, Item: item, Version: c.Version, Method: domain.PlayDirect, Started: start.Add(time.Hour)}
	if err := s.RecordPlay(ctx, p, p.Started.Add(time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	next := Before{At: first[0].StoppedAt, ID: first[0].ID}
	if second, more, err := s.History(ctx, uuid.UUID{}, next, 1); err != nil || !more || len(second) != 1 || second[0].Profile != kid.ID || second[0].StoppedAt.After(next.At) {
		t.Errorf("everyone's second = %+v, more %v, %v; want the kid's first play, as before the new one", second, more, err)
	}
}

func TestAPlayRecordNamesItsTitleByWhatAnotherServiceFindsItBy(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows", "")
	if err != nil {
		t.Fatal(err)
	}
	part := func(key string) []Version {
		return []Version{{ContentKey: []byte(key), Parts: []Part{{RelPath: key + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	}
	heat := Film{Title: "Heat", Year: 1995, Folder: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949", domain.ProviderIMDb: "tt0113277"}, Versions: part("heat")}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v1"), []Film{heat}, nil); err != nil {
		t.Fatal(err)
	}
	pilot := Episode{Season: 1, Episodes: []int{1}, Title: "The Target", Folder: "Wire/Season 1", ByNumber: true, Versions: part("wire")}
	wire := Show{Title: "The Wire", Folder: "Wire", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1438", domain.ProviderTVDB: "79126"}}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Wire/Season 1", []byte("v1"), wire, []Episode{pilot}, nil); err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	for n, play := range []struct {
		who  uuid.UUID
		kind string
	}{{oliver.ID, "movie"}, {oliver.ID, "episode"}, {kid.ID, "movie"}} {
		item := oneItem(t, s, "kind = '"+play.kind+"'")
		p := domain.Playback{ID: uuid.NewV7(), Profile: play.who, Item: item.ID, Method: domain.PlayDirect, Started: start.Add(time.Duration(n) * time.Minute)}
		if err := s.RecordPlay(ctx, p, p.Started.Add(time.Minute), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	mine, more, err := s.PlayRecords(ctx, oliver.ID, Newest, 10)
	if err != nil || more || len(mine) != 2 {
		t.Fatalf("Oliver's = %+v, more %v, %v; want his two and no more", mine, more, err)
	}
	ep, film := mine[0], mine[1]
	if ep.Kind != domain.ItemEpisode || ep.Title != "The Target" || ep.ShowTitle != "The Wire" ||
		ep.SeasonNumber == nil || *ep.SeasonNumber != 1 || ep.EpisodeNumber == nil || *ep.EpisodeNumber != 1 ||
		ep.TMDB != "1438" || ep.TVDB != "79126" || ep.IMDb != "" || ep.ProfileName != "Oliver" {
		t.Errorf("episode = %+v, want The Target of The Wire, 1x1, by the show's ids", ep)
	}
	if film.Kind != domain.ItemMovie || film.Title != "Heat" || film.Year == nil || *film.Year != 1995 || film.ShowTitle != "" ||
		film.TMDB != "949" || film.IMDb != "tt0113277" || film.TVDB != "" {
		t.Errorf("film = %+v, want Heat (1995) by its own ids", film)
	}
	if all, _, err := s.PlayRecords(ctx, uuid.UUID{}, Newest, 10); err != nil || len(all) != 3 || all[0].ProfileName != "Kid" {
		t.Errorf("everyone's = %+v, %v; want all three, the kid's latest first", all, err)
	}
}
