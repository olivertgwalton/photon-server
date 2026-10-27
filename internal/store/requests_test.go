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
	var shows domain.Library
	var err error
	for _, lib := range []struct {
		into *domain.Library
		name string
		kind domain.LibraryKind
	}{{&films, "Films", domain.LibraryMovies}, {&shows, "Shows", domain.LibraryShows}} {
		if *lib.into, err = s.AddLibrary(ctx, lib.name, lib.kind, "/srv/"+lib.name, domain.SourceTMDB); err != nil {
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

	mine, total, err := totalled(s.Requests(ctx, user.ID, totalledPage(0, 10)))
	if err != nil || total != 2 || len(mine) != 2 || mine[0].ID != seasons.ID {
		t.Errorf("the user's requests %+v of %d, %v; want both, newest first", mine, total, err)
	}
	if theirs, total, err := totalled(s.Requests(ctx, other.ID, totalledPage(0, 10))); err != nil || total != 0 || len(theirs) != 0 {
		t.Errorf("another's requests %+v of %d, %v; want none", theirs, total, err)
	}
	waiting, total, err := totalled(s.RequestsKept(ctx, nil, domain.RequestPending, totalledPage(0, 10)))
	if err != nil || total != 2 || len(waiting) != 2 {
		t.Errorf("waiting for an admin %+v of %d, %v", waiting, total, err)
	}

	if _, err := s.WithdrawRequest(ctx, other.ID, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("taken back by another: %v, want ErrNotFound", err)
	}
	if gone, err := s.WithdrawRequest(ctx, user.ID, r.ID); err != nil || gone.Title != r.Title || gone.Library != r.Library || gone.State != domain.RequestPending {
		t.Fatalf("taken back %+v, %v; want %s as it waited", gone, err, r.Title)
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

	inception := Film{Title: "Inception", Folder: "I", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}, Versions: []Version{{
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
	if kept, total, err := totalled(s.RequestsKept(ctx, &manager.ID, "", totalledPage(0, 10))); err != nil || total != 1 || kept[0].ID != r.ID {
		t.Errorf("the manager's %+v of %d, %v; want the teen's alone", kept, total, err)
	}
	if kept, total, err := totalled(s.RequestsKept(ctx, &stranger.ID, "", totalledPage(0, 10))); err != nil || total != 0 || len(kept) != 0 {
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
	if _, err := s.WithdrawRequest(ctx, child.ID, r.ID); !errors.Is(err, ErrDecided) {
		t.Errorf("taken back once declined: %v, want ErrDecided", err)
	}
	again, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatalf("asked for again once declined: %v", err)
	}
	if approved, err := s.DecideRequest(ctx, again.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil || approved.State != domain.RequestApproved {
		t.Errorf("approved by an admin %+v, %v", approved, err)
	}
	if done, total, err := totalled(s.RequestsKept(ctx, nil, domain.RequestDeclined, totalledPage(0, 10))); err != nil || total != 1 || done[0].ID != r.ID {
		t.Errorf("declined requests %+v of %d, %v; want the first", done, total, err)
	}
}

// An approved request is sent where its library's requests go, by every id it was found under;
// one that fails there says why and may be approved again, and sent again, or declined. A library
// that sends nowhere keeps its requests approved, and a remote library takes none.
func TestAnApprovedRequestIsSentWhereItsLibrarySays(t *testing.T) {
	s, films, film, show := askable(t)
	ctx := t.Context()
	admin := addProfile(t, s, "Admin", domain.RoleAdmin, nil)
	user := addProfile(t, s, "Sam", domain.RoleUser, nil)
	sends := func() int {
		t.Helper()
		return countRows(t, s, `SELECT count(*) FROM jobs WHERE kind = 'send_request'`)
	}

	unsent, err := s.AskFor(ctx, admin.ID, show.ID, nil)
	if err != nil || unsent.State != domain.RequestApproved || sends() != 0 {
		t.Fatalf("a request of a library that sends nowhere: %+v, %v, %d sends; want it approved, and none", unsent, err, sends())
	}
	riven := domain.PluginSource("riven")
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Requests: &riven}); err != nil {
		t.Fatal(err)
	}
	r, err := s.AskFor(ctx, user.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sends() != 0 {
		t.Errorf("%d sends of a request waiting for an admin", sends())
	}
	if _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil {
		t.Fatal(err)
	}
	if sends() != 1 {
		t.Errorf("%d sends once approved, want one", sends())
	}
	w, lib, ok, err := s.Sending(ctx, r.ID)
	if err != nil || !ok || lib.RequestSource != riven || w.ID != r.ID || w.Kind != domain.ItemMovie || w.IDs[domain.ProviderTMDB] != "27205" || w.Title != "Inception" {
		t.Errorf("sending %+v to %q, %v, %v", w, lib.RequestSource, ok, err)
	}

	if err := s.SentRequest(ctx, r.ID, domain.RequestFailed, "no release found"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.Sending(ctx, r.ID); err != nil || ok {
		t.Errorf("a failed request is still being sent: %v, %v", ok, err)
	}
	again, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, "")
	if err != nil || again.State != domain.RequestApproved || again.Reason != "" {
		t.Errorf("approved again once failed: %+v, %v", again, err)
	}
	if err := s.SentRequest(ctx, r.ID, domain.RequestSent, ""); err != nil {
		t.Fatal(err)
	}
	if sent, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestDeclined, ""); !errors.Is(err, ErrDecided) {
		t.Errorf("a sent request declined: %+v, %v; want ErrDecided", sent, err)
	}
	if mine, _, err := totalled(s.Requests(ctx, user.ID, totalledPage(0, 1))); err != nil || mine[0].State != domain.RequestSent {
		t.Errorf("the user's request %+v, %v; want it sent", mine, err)
	}

	found, err := s.AddRemoteLibrary(ctx, "Found", domain.LibraryMovies, Remote{DiscoverSource: domain.SourceTMDB, StreamSource: riven})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, found.ID, LibraryChange{Requests: &riven}); !errors.Is(err, ErrRemoteRequests) {
		t.Errorf("a remote library sending requests: %v, want ErrRemoteRequests", err)
	}
}

// A request is available once a library's title with the id it was found under is identified, and
// names that title; the profile that asked is told of it as a scan tells of titles added, though
// it marked nothing, unless it hears of nothing. A declined request stays declined.
func TestARequestIsAvailableOnceItsTitleIsIdentified(t *testing.T) {
	s, films, film, _ := askable(t)
	ctx := t.Context()
	admin := addProfile(t, s, "Admin", domain.RoleAdmin, nil)
	asker := addProfile(t, s, "Sam", domain.RoleUser, nil)
	r, err := s.AskFor(ctx, asker.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil {
		t.Fatal(err)
	}
	inception := Film{Title: "Inception", Folder: "I", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}, Versions: []Version{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "I/inception.mkv", Size: 1, Facts: &domain.Facts{Duration: 1}}},
	}}}
	if _, err := s.SaveFolder(ctx, films.ID, "I", []byte("v1"), []Film{inception}, nil); err != nil {
		t.Fatal(err)
	}
	item := oneItem(t, s, "title = $1", "Inception")
	told, err := s.Identified(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0].ProfileID != asker.ID {
		t.Errorf("told %+v, want the asker alone", told)
	}
	mine, _, err := totalled(s.Requests(ctx, asker.ID, totalledPage(0, 1)))
	if err != nil || mine[0].State != domain.RequestAvailable || mine[0].Item != item.ID || mine[0].AvailableAt.IsZero() {
		t.Errorf("the request %+v, %v; want it available, naming Inception", mine, err)
	}
	inbox, err := s.Inbox(ctx, asker.ID, nil)
	if err != nil || inbox.Unread != 1 || len(inbox.Notifications) != 1 {
		t.Errorf("the asker's inbox %+v, %v; want Inception in it", inbox, err)
	}
	if again, err := s.Identified(ctx, item.ID); err != nil || len(again) != 0 {
		t.Errorf("identified again: told %+v, %v; want no one twice", again, err)
	}
}

// A title may come while its request still waits, which no one then decides: it is available, and
// a profile that hears of nothing is not told of it.
func TestARequestIsAvailableQuietlyWhereItsAskerHearsNothing(t *testing.T) {
	s, films, film, _ := askable(t)
	ctx := t.Context()
	quiet := addProfile(t, s, "Quiet", domain.RoleUser, nil)
	prefs := domain.DefaultPreferences()
	prefs.Notify = domain.NotifyNone
	if _, err := s.SetPreferences(ctx, quiet.ID, prefs); err != nil {
		t.Fatal(err)
	}
	r, err := s.AskFor(ctx, quiet.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	inception := Film{Title: "Inception", Folder: "I", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}, Versions: []Version{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "I/inception.mkv", Size: 1, Facts: &domain.Facts{Duration: 1}}},
	}}}
	if _, err := s.SaveFolder(ctx, films.ID, "I", []byte("v1"), []Film{inception}, nil); err != nil {
		t.Fatal(err)
	}
	told, err := s.Identified(ctx, oneItem(t, s, "title = $1", "Inception").ID)
	if err != nil || len(told) != 0 {
		t.Errorf("told %+v, %v; want no one", told, err)
	}
	if mine, _, err := totalled(s.Requests(ctx, quiet.ID, totalledPage(0, 1))); err != nil || mine[0].ID != r.ID || mine[0].State != domain.RequestAvailable {
		t.Errorf("the request %+v, %v; want it available", mine, err)
	}
}
