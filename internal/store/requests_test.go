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

	if _, _, err := s.AskFor(ctx, user.ID, film.ID, []int{1}); !errors.Is(err, ErrFilmSeasons) {
		t.Errorf("a film by seasons: %v, want ErrFilmSeasons", err)
	}
	r, _, err := s.AskFor(ctx, user.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != domain.RequestPending || r.Library != films.ID || r.Title != "Inception" || r.Year != 2010 ||
		r.Provider != domain.ProviderTMDB || r.Value != "27205" || r.ProfileName != "Sam" || r.Seasons != nil {
		t.Errorf("asked for %+v", r)
	}
	if _, _, err := s.AskFor(ctx, other.ID, film.ID, nil); !errors.Is(err, ErrAskedFor) {
		t.Errorf("asked for again: %v, want ErrAskedFor", err)
	}
	seasons, _, err := s.AskFor(ctx, user.ID, show.ID, []int{3, 1, 3})
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
	if _, err := s.WithdrawRequest(ctx, user.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AskFor(ctx, other.ID, film.ID, nil); err != nil {
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
	if _, _, err := s.AskFor(ctx, user.ID, streamed[0].ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a remote library's find: %v, want ErrNotFound", err)
	}
	if _, _, err := s.AskFor(ctx, user.ID, uuid.NewV7(), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no find: %v, want ErrNotFound", err)
	}

	kid := addProfile(t, s, "Kid", domain.RoleUser, nil)
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AskFor(ctx, kid.ID, film.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a profile held to 12: %v, want ErrNotFound", err)
	}
	filmsOnly := addProfile(t, s, "Films only", domain.RoleUser, nil)
	if err := s.SetAccess(ctx, filmsOnly.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AskFor(ctx, filmsOnly.ID, show.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a find of a library the profile may not see: %v, want ErrNotFound", err)
	}

	inception := Film{Title: "Inception", Folder: "I", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}, Versions: []Version{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "I/inception.mkv", Size: 1, Facts: &domain.Facts{Duration: 1}}},
	}}}
	if _, err := s.SaveFolder(ctx, films.ID, "I", []byte("v1"), []Film{inception}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AskFor(ctx, user.ID, film.ID, nil); !errors.Is(err, ErrAlreadyHeld) {
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

	own, _, err := s.AskFor(ctx, admin.ID, show.ID, nil)
	if err != nil || own.State != domain.RequestApproved || own.DecidedBy != admin.ID || own.DecidedAt.IsZero() {
		t.Errorf("an admin's own request %+v, %v; want it approved by the admin", own, err)
	}

	r, _, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kept, total, err := totalled(s.RequestsKept(ctx, &manager.ID, "", totalledPage(0, 10))); err != nil || total != 1 || kept[0].ID != r.ID {
		t.Errorf("the manager's %+v of %d, %v; want the teen's alone", kept, total, err)
	}
	if kept, total, err := totalled(s.RequestsKept(ctx, &stranger.ID, "", totalledPage(0, 10))); err != nil || total != 0 || len(kept) != 0 {
		t.Errorf("another manager's %+v of %d, %v; want none", kept, total, err)
	}
	if _, _, err := s.DecideRequest(ctx, r.ID, &stranger.ID, stranger.ID, domain.RequestApproved, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("approved by another manager: %v, want ErrNotFound", err)
	}
	declined, _, err := s.DecideRequest(ctx, r.ID, &manager.ID, manager.ID, domain.RequestDeclined, "Not until you're older")
	if err != nil || declined.State != domain.RequestDeclined || declined.Reason != "Not until you're older" || declined.DecidedBy != manager.ID {
		t.Errorf("declined %+v, %v", declined, err)
	}
	if _, _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); !errors.Is(err, ErrDecided) {
		t.Errorf("decided again: %v, want ErrDecided", err)
	}
	if _, err := s.WithdrawRequest(ctx, child.ID, r.ID); !errors.Is(err, ErrDecided) {
		t.Errorf("taken back once declined: %v, want ErrDecided", err)
	}
	again, _, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatalf("asked for again once declined: %v", err)
	}
	if approved, _, err := s.DecideRequest(ctx, again.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil || approved.State != domain.RequestApproved {
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

	unsent, _, err := s.AskFor(ctx, admin.ID, show.ID, nil)
	if err != nil || unsent.State != domain.RequestApproved || sends() != 0 {
		t.Fatalf("a request of a library that sends nowhere: %+v, %v, %d sends; want it approved, and none", unsent, err, sends())
	}
	riven := domain.PluginSource("riven")
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Requests: &riven}); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.AskFor(ctx, user.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sends() != 0 {
		t.Errorf("%d sends of a request waiting for an admin", sends())
	}
	if _, _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil {
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
	again, _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, "")
	if err != nil || again.State != domain.RequestApproved || again.Reason != "" {
		t.Errorf("approved again once failed: %+v, %v", again, err)
	}
	if err := s.SentRequest(ctx, r.ID, domain.RequestSent, ""); err != nil {
		t.Fatal(err)
	}
	if sent, _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestDeclined, ""); !errors.Is(err, ErrDecided) {
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
	r, _, err := s.AskFor(ctx, asker.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DecideRequest(ctx, r.ID, nil, admin.ID, domain.RequestApproved, ""); err != nil {
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
	if len(told.Told) != 1 || told.Told[0].ProfileID != asker.ID || len(told.Cleared) != 0 {
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
	if again, err := s.Identified(ctx, item.ID); err != nil || len(again.Told) != 0 {
		t.Errorf("identified again: told %+v, %v; want no one twice", again, err)
	}
}

// A title may come while its request still waits, which no one then decides: it is available, a
// profile that hears of nothing is not told of it, and the admin told it waits no longer is.
func TestARequestIsAvailableQuietlyWhereItsAskerHearsNothing(t *testing.T) {
	s, films, film, _ := askable(t)
	ctx := t.Context()
	admin := addProfile(t, s, "Admin", domain.RoleAdmin, nil)
	quiet := addProfile(t, s, "Quiet", domain.RoleUser, nil)
	prefs := domain.DefaultPreferences()
	prefs.Notify = domain.NotifyNone
	if _, err := s.SetPreferences(ctx, quiet.ID, prefs); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.AskFor(ctx, quiet.ID, film.ID, nil)
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
	if err != nil || len(told.Told) != 0 {
		t.Errorf("told %+v, %v; want no one", told, err)
	}
	if len(told.Cleared) != 1 || told.Cleared[0].ProfileID != admin.ID || told.Cleared[0].Unread != 0 {
		t.Errorf("cleared %+v, want the admin's notification of the request, none left unread", told.Cleared)
	}
	if inbox, err := s.Inbox(ctx, admin.ID, nil); err != nil || len(inbox.Notifications) != 0 {
		t.Errorf("the admin's inbox %+v, %v; want the request gone from it", inbox, err)
	}
	if mine, _, err := totalled(s.Requests(ctx, quiet.ID, totalledPage(0, 1))); err != nil || mine[0].ID != r.ID || mine[0].State != domain.RequestAvailable {
		t.Errorf("the request %+v, %v; want it available", mine, err)
	}
}

// A request that waits is told to each who may decide it: every admin, and the manager that keeps
// the user who asked; not another manager, the one who asked, nor an admin who hears of nothing.
// It stays in their inboxes until it no longer waits, taken back or decided by any of them, when
// it is cleared from every one. An admin's own, approved as it is asked, is told to no one.
func TestARequestWaitingIsToldToWhoeverDecidesIt(t *testing.T) {
	s, _, film, show := askable(t)
	ctx := t.Context()
	admin := addProfile(t, s, "Admin", domain.RoleAdmin, nil)
	hushed := addProfile(t, s, "Hushed", domain.RoleAdmin, nil)
	prefs := domain.DefaultPreferences()
	prefs.Notify = domain.NotifyNone
	if _, err := s.SetPreferences(ctx, hushed.ID, prefs); err != nil {
		t.Fatal(err)
	}
	manager := addProfile(t, s, "Parent", domain.RoleManager, nil)
	stranger := addProfile(t, s, "Neighbour", domain.RoleManager, nil)
	child := addProfile(t, s, "Teen", domain.RoleUser, &manager.ID)

	r, told, err := s.AskFor(ctx, child.ID, film.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[uuid.UUID][]int64{}
	for _, n := range told {
		ids[n.ProfileID] = n.IDs
	}
	if len(ids) != 2 || len(ids[admin.ID]) != 1 || len(ids[manager.ID]) != 1 {
		t.Fatalf("told %+v, want the admin and the parent once each", told)
	}
	for _, p := range []domain.Profile{admin, manager} {
		inbox, err := s.Inbox(ctx, p.ID, nil)
		if err != nil || inbox.Unread != 1 || len(inbox.Notifications) != 1 {
			t.Fatalf("%s's inbox %+v, %v; want the request, unread", p.Name, inbox, err)
		}
		if n := inbox.Notifications[0]; n.ID != ids[p.ID][0] || n.Kind != domain.NotificationRequest || n.Request != r.ID ||
			inbox.Requests[r.ID].ProfileName != "Teen" || inbox.Requests[r.ID].Title != "Inception" {
			t.Errorf("%s's notification %+v of %+v; want the teen's Inception", p.Name, n, inbox.Requests)
		}
	}
	for _, p := range []domain.Profile{hushed, stranger, child} {
		if inbox, err := s.Inbox(ctx, p.ID, nil); err != nil || inbox.Unread != 0 || len(inbox.Notifications) != 0 {
			t.Errorf("%s's inbox %+v, %v; want nothing", p.Name, inbox, err)
		}
	}

	// A manager's own request waits for the admins alone, and taken back is cleared.
	asked, told, err := s.AskFor(ctx, manager.ID, show.ID, nil)
	if err != nil || len(told) != 1 || told[0].ProfileID != admin.ID {
		t.Fatalf("the parent's own request told %+v, %v; want the admin alone", told, err)
	}
	cleared, err := s.WithdrawRequest(ctx, manager.ID, asked.ID)
	if err != nil || len(cleared) != 1 || cleared[0].ProfileID != admin.ID || !slices.Equal(cleared[0].IDs, told[0].IDs) || cleared[0].Unread != 1 {
		t.Errorf("taken back, cleared %+v, %v; want the admin's, the teen's left unread", cleared, err)
	}

	_, cleared, err = s.DecideRequest(ctx, r.ID, &manager.ID, manager.ID, domain.RequestApproved, "")
	if err != nil {
		t.Fatal(err)
	}
	left := map[uuid.UUID]Cleared{}
	for _, c := range cleared {
		left[c.ProfileID] = c
	}
	if len(left) != 2 || !slices.Equal(left[admin.ID].IDs, ids[admin.ID]) || !slices.Equal(left[manager.ID].IDs, ids[manager.ID]) ||
		left[admin.ID].Unread != 0 || left[manager.ID].Unread != 0 {
		t.Errorf("decided, cleared %+v; want the admin's and the parent's, none left unread", cleared)
	}
	for _, p := range []domain.Profile{admin, manager} {
		if inbox, err := s.Inbox(ctx, p.ID, nil); err != nil || inbox.Unread != 0 || len(inbox.Notifications) != 0 {
			t.Errorf("%s's inbox once decided %+v, %v; want it empty", p.Name, inbox, err)
		}
	}

	if _, told, err := s.AskFor(ctx, admin.ID, show.ID, nil); err != nil || len(told) != 0 {
		t.Errorf("an admin's own request told %+v, %v; want no one", told, err)
	}
}
