package historyimport

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var watchedAt = time.Date(2024, 3, 9, 20, 15, 0, 0, time.UTC)

func plexLibrary() *fakePlex {
	return &fakePlex{
		token: "plex-token",
		films: []object{
			{
				"ratingKey": "10", "title": "The Matrix", "guid": "plex://movie/5d77", "viewCount": 2, "lastViewedAt": watchedAt.Unix(),
				"Guid": []object{{"id": "imdb://tt0133093"}, {"id": "tmdb://603"}},
			},
			{"ratingKey": "11", "title": "Unseen", "guid": "plex://movie/5d78", "Guid": []object{{"id": "tmdb://1"}}},
		},
		shows: []object{
			{"ratingKey": "20", "title": "The Wire", "guid": "plex://show/5d79", "Guid": []object{{"id": "tvdb://79126"}}},
		},
		episodes: []object{
			{
				"ratingKey": "21", "title": "The Target", "grandparentTitle": "The Wire", "grandparentRatingKey": "20",
				"parentIndex": 1, "index": 1, "viewOffset": 1_200_000, "lastViewedAt": watchedAt.Unix(),
			},
		},
	}
}

func TestAPlexServersWatchedFilmsAndStartedEpisodesAreRead(t *testing.T) {
	f := plexLibrary()
	base := f.serve(t)
	if _, err := connect(t.Context(), domain.ImportPlex, base, Credentials{Token: "wrong"}); !errors.Is(err, ErrRefused) ||
		!strings.Contains(err.Error(), "refused the credentials") {
		t.Fatalf("a wrong token: err = %v, want it refused", err)
	}
	login, err := connect(t.Context(), domain.ImportPlex, base, Credentials{Token: f.token})
	if err != nil {
		t.Fatal(err)
	}
	got, err := open(domain.ImportPlex, base, login).entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %+v, want the watched film and the started episode", got)
	}
	film, episode := got[0], got[1]
	if film.kind != domain.ItemMovie || film.plays != 2 || !film.at.Equal(watchedAt) ||
		!maps.Equal(film.ids, map[domain.Provider]string{domain.ProviderTMDB: "603", domain.ProviderIMDb: "tt0133093"}) {
		t.Errorf("film = %+v, want The Matrix watched twice by its ids", film)
	}
	if episode.kind != domain.ItemEpisode || episode.season != 1 || episode.episode != 1 || episode.plays != 0 ||
		episode.position != 20*time.Minute || episode.ids[domain.ProviderTVDB] != "79126" || episode.title != "The Wire S01E01" {
		t.Errorf("episode = %+v, want The Wire S01E01 started 20 minutes in, by its show's ids", episode)
	}
}

func jellyfinLibrary(emby bool) *fakeJellyfin {
	date := "2024-03-09T20:15:00.0000000Z"
	if emby {
		date = "2024-03-09T20:15:00.0000000"
	}
	return &fakeJellyfin{
		emby: emby, user: "ada", password: "secret",
		series: []object{{"Id": "s1", "Name": "The Wire", "Type": "Series", "ProviderIds": object{"Tvdb": "79126"}}},
		movies: []object{
			{
				"Id": "m1", "Name": "The Matrix", "Type": "Movie", "ProviderIds": object{"Tmdb": "603", "Imdb": "tt0133093"},
				"UserData": object{"Played": true, "PlayCount": 0, "LastPlayedDate": date},
			},
		},
		episodes: []object{
			{
				"Id": "e1", "Name": "The Target", "Type": "Episode", "SeriesId": "s1", "SeriesName": "The Wire",
				"ParentIndexNumber": 1, "IndexNumber": 1, "ProviderIds": object{},
				"UserData": object{"Played": true, "PlayCount": 3, "PlaybackPositionTicks": int64(12_000_000_000), "LastPlayedDate": date},
			},
		},
	}
}

func TestAJellyfinOrEmbyUsersWatchedTitlesAreRead(t *testing.T) {
	for _, kind := range []domain.ImportSource{domain.ImportJellyfin, domain.ImportEmby} {
		t.Run(string(kind), func(t *testing.T) {
			f := jellyfinLibrary(kind == domain.ImportEmby)
			base := f.serve(t)
			if _, err := connect(t.Context(), kind, base, Credentials{Username: "ada", Password: "nope"}); !errors.Is(err, ErrRefused) {
				t.Fatalf("a wrong password: err = %v, want it refused", err)
			}
			login, err := connect(t.Context(), kind, base, Credentials{Username: "ada", Password: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			src := open(kind, base, login)
			got, err := src.entries(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			src.signOut(t.Context())
			if len(got) != 2 {
				t.Fatalf("entries = %+v, want the film and the episode, once each", got)
			}
			film, episode := got[0], got[1]
			if film.plays != 1 || !film.at.Equal(watchedAt) || film.ids[domain.ProviderTMDB] != "603" {
				t.Errorf("film = %+v, want The Matrix marked played, once, at its date", film)
			}
			if episode.kind != domain.ItemEpisode || episode.plays != 3 || episode.position != 20*time.Minute ||
				episode.ids[domain.ProviderTVDB] != "79126" || episode.season != 1 || episode.episode != 1 {
				t.Errorf("episode = %+v, want S01E01 of The Wire by its show's ids, played thrice and 20 minutes into another", episode)
			}
			if f.signOuts != 1 {
				t.Errorf("sign-outs = %d, want the session ended", f.signOuts)
			}
		})
	}
}
