//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// askable is a server with a library of films and one of shows that search TMDB, and the finds of
// each a profile may ask for.
func askable(t *testing.T) (s *Store, films domain.Library, film, show Discovery) {
	t.Helper()
	s = migrated(t)
	ctx := t.Context()
	tmdb := domain.SourceTMDB
	var shows domain.Library
	var err error
	for _, lib := range []struct {
		into *domain.Library
		name string
		kind domain.LibraryKind
	}{{&films, "Films", domain.LibraryMovies}, {&shows, "Shows", domain.LibraryShows}} {
		if *lib.into, err = s.AddLibrary(ctx, lib.name, lib.kind, "/srv/"+lib.name); err != nil {
			t.Fatal(err)
		}
		if err := s.SetLibrary(ctx, lib.into.ID, LibraryChange{Discover: &tmdb}); err != nil {
			t.Fatal(err)
		}
	}
	found, err := s.SaveDiscoveries(ctx, films.ID, domain.ItemMovie, domain.ProviderTMDB, []domain.Candidate{{ID: "27205", Title: "Inception", Year: 2010}})
	if err != nil {
		t.Fatal(err)
	}
	film = found[0]
	if found, err = s.SaveDiscoveries(ctx, shows.ID, domain.ItemShow, domain.ProviderTMDB, []domain.Candidate{{ID: "1396", Title: "Breaking Bad", Year: 2008}}); err != nil {
		t.Fatal(err)
	}
	return s, films, film, found[0]
}

func addProfile(t *testing.T, s *Store, name string, role domain.Role, by *uuid.UUID) domain.Profile {
	t.Helper()
	p, err := s.AddProfile(t.Context(), name, role, "hash", by)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A user asks for a title a library of files' search found, and it waits, among the user's own and
// for an admin; a show is asked for by its seasons. The title is asked for once while it waits,
// whoever asks, and the user may take it back until it is decided.
func TestAProfileAsksForATitleALibraryLacks(t *testing.T) {
	s, films, film, show := askable(t)
	ctx := t.Context()
	user := addProfile(t, s, "Sam", domain.RoleUser, nil)
	other := addProfile(t, s, "Alex", domain.RoleUser, nil)

	if _, err := s.AskFor(ctx, user.ID, film.ID, []int{1}); !errors.Is(err, ErrFilmSeasons) {
		t.Errorf("a film by seasons: %v, want ErrFilmSeasons", err)
	}
	r, err := s.AskFor(ctx, user.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != domain.RequestPending || r.Library != films.ID || r.Title != "Inception" || r.Year != 2010 ||
		r.Provider != domain.ProviderTMDB || r.Value != "27205" || r.ProfileName != "Sam" || r.Seasons != nil {
		t.Errorf("asked for %+v", r)
	}
	if _, err := s.AskFor(ctx, other.ID, film.ID, nil); !errors.Is(err, ErrAskedFor) {
		t.Errorf("asked for again: %v, want ErrAskedFor", err)
	}
	seasons, err := s.AskFor(ctx, user.ID, show.ID, []int{3, 1, 3})
	if err != nil || !slices.Equal(seasons.Seasons, []int{1, 3}) {
		t.Errorf("a show by seasons: %+v, %v; want seasons 1 and 3", seasons, err)
	}

	mine, total, err := s.Requests(ctx, user.ID, 0, 10)
	if err != nil || total != 2 || len(mine) != 2 || mine[0].ID != seasons.ID {
		t.Errorf("the user's requests %+v of %d, %v; want both, newest first", mine, total, err)
	}
	if theirs, total, err := s.Requests(ctx, other.ID, 0, 10); err != nil || total != 0 || len(theirs) != 0 {
		t.Errorf("another's requests %+v of %d, %v; want none", theirs, total, err)
	}
	waiting, total, err := s.RequestsKept(ctx, nil, domain.RequestPending, 0, 10)
	if err != nil || total != 2 || len(waiting) != 2 {
		t.Errorf("waiting for an admin %+v of %d, %v", waiting, total, err)
	}

	if err := s.WithdrawRequest(ctx, other.ID, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("taken back by another: %v, want ErrNotFound", err)
	}
	if err := s.WithdrawRequest(ctx, user.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskFor(ctx, other.ID, film.ID, nil); err != nil {
		t.Errorf("asked for once taken back: %v", err)
	}
}

// What a profile cannot search it cannot ask for: a remote library's find, which is opened instead,
// a find for a profile held to an age, or one of a library it may not see. A title a library has
// come to hold since it was found is refused.
func TestOnlyWhatAProfileMaySearchIsAskedFor(t *testing.T) {
	s, films, film, show := askable(t)
	ctx := t.Context()
	user := addProfile(t, s, "Sam", domain.RoleUser, nil)

	remote, err := s.AddRemoteLibrary(ctx, "Found", domain.LibraryMovies, Remote{DiscoverSource: domain.SourceTMDB, StreamSource: domain.PluginSource("riven")})
	if err != nil {
		t.Fatal(err)
	}
	streamed, err := s.SaveDiscoveries(ctx, remote.ID, domain.ItemMovie, domain.ProviderTMDB, []domain.Candidate{{ID: "155", Title: "The Dark Knight"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskFor(ctx, user.ID, streamed[0].ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a remote library's find: %v, want ErrNotFound", err)
	}
	if _, err := s.AskFor(ctx, user.ID, uuid.NewV7(), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no find: %v, want ErrNotFound", err)
	}

	kid := addProfile(t, s, "Kid", domain.RoleUser, nil)
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskFor(ctx, kid.ID, film.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a profile held to 12: %v, want ErrNotFound", err)
	}
	filmsOnly := addProfile(t, s, "Films only", domain.RoleUser, nil)
	if err := s.SetAccess(ctx, filmsOnly.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskFor(ctx, filmsOnly.ID, show.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a find of a library the profile may not see: %v, want ErrNotFound", err)
	}

	inception := Film{Title: "Inception", Folder: "I", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}, Copies: []Copy{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "I/inception.mkv", Size: 1, Facts: &domain.Facts{Duration: 1}}},
	}}}
	if _, err := s.SaveFolder(ctx, films.ID, "I", []byte("v1"), []Film{inception}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskFor(ctx, user.ID, film.ID, nil); !errors.Is(err, ErrAlreadyHeld) {
		t.Errorf("a film a library has come to hold: %v, want ErrAlreadyHeld", err)
	}
}

// An admin's own request is approved as it is asked. Another's waits for an admin, or the manager
// that keeps its profile, to approve or decline it, saying why; another manager sees none of it.
// Once decided it is neither decided again nor taken back, and a title declined may be asked for
// again.
func TestARequestIsApprovedOrDeclinedByWhoKeepsItsProfile(t *testing.T) {
	s, _, film, show := askable(t)
	ctx := t.Context()
	admin := addProfile(t, s, "Admin", domain.RoleAdmin, nil)
	manager := addProfile(t, s, "Parent", domain.RoleManager, nil)
	stranger := addProfile(t, s, "Neighbour", domain.RoleManager, nil)
	child := addProfile(t, s, "Teen", domain.RoleUser, &manager.ID)

	own, err := s.AskFor(ctx, admin.ID, show.ID, nil)
	if err != nil || own.State != domain.RequestApproved || own.DecidedBy != admin.ID || own.DecidedAt.IsZero() {
		t.Errorf("an admin's own request %+v, %v; want it approved by the admin", own, err)
	}

	r, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kept, total, err := s.RequestsKept(ctx, &manager.ID, "", 0, 10); err != nil || total != 1 || kept[0].ID != r.ID {
		t.Errorf("the manager's %+v of %d, %v; want the teen's alone", kept, total, err)
	}
	if kept, total, err := s.RequestsKept(ctx, &stranger.ID, "", 0, 10); err != nil || total != 0 || len(kept) != 0 {
		t.Errorf("another manager's %+v of %d, %v; want none", kept, total, err)
	}
	if _, err := s.DecideRequest(ctx, r.ID, &stranger.ID, stranger.ID, domain.RequestApproved, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("approved by another manager: %v, want ErrNotFound", err)
	}
	declined, err := s.DecideRequest(ctx, r.ID, &manager.ID, manager.ID, domain.RequestDeclined, "Not until you're older")
	if err != nil || declined.State != domain.RequestDeclined || declined.Reason != "Not until you're older" || declined.DecidedBy != manager.ID {
		t.Errorf("declined %+v, %v", declined, err)
	}
	if _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); !errors.Is(err, ErrDecided) {
		t.Errorf("decided again: %v, want ErrDecided", err)
	}
	if err := s.WithdrawRequest(ctx, child.ID, r.ID); !errors.Is(err, ErrDecided) {
		t.Errorf("taken back once declined: %v, want ErrDecided", err)
	}
	again, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatalf("asked for again once declined: %v", err)
	}
	if approved, err := s.DecideRequest(ctx, again.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil || approved.State != domain.RequestApproved {
		t.Errorf("approved by an admin %+v, %v", approved, err)
	}
	if done, total, err := s.RequestsKept(ctx, nil, domain.RequestDeclined, 0, 10); err != nil || total != 1 || done[0].ID != r.ID {
		t.Errorf("declined requests %+v of %d, %v; want the first", done, total, err)
	}
}
