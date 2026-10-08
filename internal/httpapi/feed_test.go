//go:build integration

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// tokenAuth signs each token in as its profile.
type tokenAuth struct {
	fakeAuth
	tokens map[string]domain.Profile
}

func (a tokenAuth) Authenticate(_ context.Context, token string) (domain.Session, error) {
	p, ok := a.tokens[token]
	if !ok {
		return domain.Session{}, auth.ErrUnauthenticated
	}
	return domain.Session{ID: uuid.NewV7(), Profile: p}, nil
}

// told is an event as a stream sends it, its details as a client reads them.
type told struct {
	name string
	eventJSON
	Details map[string]any `json:"details"`
}

func TestAProfileIsToldWhatChangesOfWhatItSees(t *testing.T) {
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
	ctx := t.Context()
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	title := map[string]uuid.UUID{}
	for _, f := range []struct {
		lib        uuid.UUID
		name, cert string
	}{{films.ID, "Paddington", "PG"}, {films.ID, "Heat", "15"}, {other.ID, "Up", "U"}} {
		saved, err := st.SaveFolder(ctx, f.lib, f.name, []byte("v1"), []store.Film{{Title: f.name, Folder: f.name, Copies: []store.Copy{{
			ContentKey: []byte(f.name), Parts: []store.Part{{RelPath: f.name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
		}}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		title[f.name] = saved.Titles[domain.TitleAdded][0]
		if err := st.SaveIdentity(ctx, title[f.name], domain.SourceTMDB, domain.Metadata{Certificate: f.cert}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// The same Paddington is in Other, which Sam may not see.
	if _, err := st.SaveFolder(ctx, other.ID, "Paddington", []byte("v1"), []store.Film{{Title: "Paddington", Folder: "Paddington", Copies: []store.Copy{{
		ContentKey: []byte("Paddington"), Parts: []store.Part{{RelPath: "Paddington.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}}, nil); err != nil {
		t.Fatal(err)
	}
	oliver, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	sam, err := st.AddProfile(ctx, "Sam", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	twelve := 12
	if err := st.SetAccess(ctx, sam.ID, store.ProfileAccess{MaxAge: &twelve, Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}

	hub := node(t, st)
	running, shutDown := context.WithCancel(ctx)
	defer shutDown()
	go hub.Run(running)
	srv := httptest.NewServer(New(log, domain.Info{}, Services{
		Auth:   tokenAuth{tokens: map[string]domain.Profile{"oliver": oliver, "sam-tv": sam, "sam-phone": sam}},
		Events: hub, Audience: st, Watching: st,
	}))
	t.Cleanup(srv.Close)
	open := func(token string) <-chan told {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("%s: %d %q", token, res.StatusCode, res.Header.Get("Content-Type"))
		}
		out := make(chan told, 64)
		go func() {
			defer close(out)
			for e := range stream(bufio.NewReader(res.Body)) {
				var got told
				if err := json.Unmarshal([]byte(e.data), &got); err != nil {
					return
				}
				got.name = e.name
				out <- got
			}
		}()
		if first := <-out; first.name != "hello" {
			t.Fatalf("%s's stream began with %q, want hello", token, first.name)
		}
		return out
	}
	// until reads a stream to the event done says it waited for, answering what came before it.
	until := func(s <-chan told, raise func(), done func(told) bool) []told {
		t.Helper()
		var seen []told
		again := time.NewTicker(200 * time.Millisecond)
		defer again.Stop()
		deadline := time.After(15 * time.Second)
		for raise(); ; {
			select {
			case e, ok := <-s:
				if !ok {
					t.Fatal("the stream ended")
				}
				if done(e) {
					return seen
				}
				seen = append(seen, e)
			case <-again.C:
				raise()
			case <-deadline:
				t.Fatalf("waited in vain, having seen %+v", seen)
			}
		}
	}
	// settle reads a stream to a change of its profile's own told after everything before it,
	// answering what came before.
	settle := func(s <-chan told, p domain.Profile) []told {
		t.Helper()
		mark := uuid.NewV7()
		return until(s, func() {
			hub.Raise(ctx, domain.Event{Kind: domain.EventUserDataChanged, Profile: p.ID, Item: mark})
		}, func(e told) bool { return e.TitleID == mark })
	}
	oliverTold, samTold := open("oliver"), open("sam-tv")
	// The node hears Valkey once it has subscribed, which a stream opening does not wait for.
	settle(samTold, sam)
	settle(oliverTold, oliver)

	hub.Changed(ctx, other.ID, store.Changed{domain.TitleAdded: {title["Up"]}})
	hub.Changed(ctx, films.ID, store.Changed{domain.TitleAdded: {title["Paddington"], title["Heat"]}})
	hub.Raise(ctx, domain.Event{Kind: domain.EventTitleUpdated, Item: title["Heat"]})
	hub.Scanning(ctx)(domain.ScanProgress{Library: other.ID, Phase: domain.ScanReading, Done: 1, Known: 2})
	hub.Raise(ctx, domain.Event{Kind: domain.EventLibraryScanned, Library: films.ID})
	defer hub.Scanned(ctx, other.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, srv.URL+"/api/v1/titles/"+title["Paddington"].String()+"/progress", strings.NewReader(`{"position_ms": 60000}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer sam-phone")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("progress from the phone: %d", res.StatusCode)
	}

	stopped := uuid.NewV7()
	hub.Raise(ctx, domain.Event{
		Kind: domain.EventPlaybackStopped, Profile: sam.ID, Item: title["Paddington"],
		Details: domain.PlaybackDetails{Playback: domain.NowPlaying{ID: stopped}},
	})

	// Oliver sees every library: once both changes reach him, Sam's stream has been handed them too.
	libraries := map[uuid.UUID]told{}
	oliverSaw := until(oliverTold, func() {}, func(e told) bool {
		if e.name == string(domain.EventLibraryChanged) {
			libraries[e.LibraryID] = e
		}
		return len(libraries) == 2
	})
	oliverSaw = append(oliverSaw, settle(oliverTold, oliver)...)
	if added, _ := libraries[films.ID].Details["added"].([]any); len(added) != 2 {
		t.Errorf("Oliver was told Films gained %v, want Paddington and Heat", libraries[films.ID].Details)
	}
	if !slices.ContainsFunc(oliverSaw, func(e told) bool { return e.name == string(domain.EventScanProgress) && e.LibraryID == other.ID }) {
		t.Error("Oliver was not told of Other's scan")
	}
	if slices.ContainsFunc(oliverSaw, func(e told) bool {
		return e.name == string(domain.EventUserDataChanged) || e.name == string(domain.EventPlaybackStopped)
	}) {
		t.Errorf("Oliver was told of Sam's progress or playback: %+v", oliverSaw)
	}

	samSaw := settle(samTold, sam)
	var changed, progress, scanned, stops []told
	for _, e := range samSaw {
		switch {
		case e.LibraryID == other.ID || e.TitleID == title["Heat"] || e.TitleID == title["Up"]:
			t.Errorf("Sam was told %s of %v, which Sam may not see", e.name, e.eventJSON)
		case e.name == string(domain.EventLibraryChanged):
			changed = append(changed, e)
		case e.name == string(domain.EventUserDataChanged) && e.TitleID == title["Paddington"]:
			progress = append(progress, e)
		case e.name == string(domain.EventLibraryScanned):
			scanned = append(scanned, e)
		case e.name == string(domain.EventPlaybackStopped):
			stops = append(stops, e)
		}
	}
	if len(scanned) != 1 || scanned[0].LibraryID != films.ID {
		t.Errorf("Sam was told of scans ending %+v, want Films's: a wall stops saying it is being scanned", scanned)
	}
	if len(changed) != 1 || changed[0].LibraryID != films.ID {
		t.Fatalf("Sam was told of library changes %+v, want Films's", changed)
	}
	if added, _ := changed[0].Details["added"].([]any); len(added) != 1 || added[0] != title["Paddington"].String() {
		t.Errorf("Sam was told Films gained %v, want Paddington alone: Heat is rated 15", changed[0].Details)
	}
	if len(progress) != 1 {
		t.Fatalf("Sam's television was told of Paddington's progress %d times, want once", len(progress))
	}
	if same, _ := progress[0].Details["title_ids"].([]any); len(same) != 1 || same[0] != title["Paddington"].String() {
		t.Errorf("Sam was told the progress is of %v, want Films' Paddington alone: Other's is the same but unseen", progress[0].Details)
	}
	if len(stops) != 1 || len(stops[0].Details) != 1 || stops[0].Details["playback_id"] != stopped.String() {
		t.Errorf("Sam was told of stops %+v, want the one playback's id alone, for its player to close", stops)
	}
}
