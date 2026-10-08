package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// An admin is told, for every node running, whether it can read a library's root: one there, one
// removed, a file in its place, and a node that does not answer. A member is refused.
func TestAnAdminChecksEveryNodeCanReadALibrarysRoot(t *testing.T) {
	key, err := nodecall.NewKey([]byte("cluster signing key"))
	if err != nil {
		t.Fatal(err)
	}
	there := t.TempDir()
	for _, name := range []string{"Heat (1995)", "Alien (1979)"} {
		if err := os.Mkdir(filepath.Join(there, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removed := t.TempDir()
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "films.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	library := func(root string) domain.Library {
		return domain.Library{ID: uuid.NewV7(), Name: "Films", Kind: domain.LibraryMovies, Root: root}
	}
	readable, missing, notAFolder := library(there), library(removed), library(file)
	libs := &fakeLibraries{libs: []domain.Library{readable, missing, notAFolder}}

	other := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Libraries: libs, NodeKey: key}))
	defer other.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	self, beta, gamma := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Libraries: libs, NodeKey: key,
		Valkey: fakeBackend{nodes: []domain.Node{
			{ID: gamma, Name: "gamma", Address: gone.URL, Role: domain.NodeAll, Availability: domain.NodeActive},
			{ID: beta, Name: "beta", Address: other.URL, Role: domain.NodeAll, Availability: domain.NodeActive},
		}},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node {
			return domain.Node{ID: self, Name: "alpha", Role: domain.NodeAll, Availability: domain.NodeActive}
		}, nil, key),
	})
	check := func(token string, lib domain.Library) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/libraries/"+lib.ID.String()+"/check", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := check(memberToken, readable); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	for _, tc := range []struct {
		lib     domain.Library
		want    rootAccess
		entries int
	}{
		{readable, accessReadable, 2},
		{missing, accessMissing, 0},
		{notAFolder, accessNotAFolder, 0},
	} {
		rec := check(goodToken, tc.lib)
		var got libraryCheckJSON
		if rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
			t.Fatalf("%s: %d %s", tc.lib.Root, rec.Code, rec.Body)
		}
		if got.Root != tc.lib.Root || len(got.Nodes) != 3 {
			t.Fatalf("%s: %+v, want alpha, beta and gamma", tc.lib.Root, got)
		}
		for _, n := range got.Nodes[:2] {
			if n.Access != tc.want || n.Entries != tc.entries || (tc.want != accessReadable) != (n.Error != "") {
				t.Errorf("%s on %s: %+v, want %s with %d entries", tc.lib.Root, n.Name, n.rootCheckJSON, tc.want, tc.entries)
			}
		}
		if g := got.Nodes[2]; g.ID != gamma || g.Access != accessUnreachableNode || g.Error == "" {
			t.Errorf("%s on the node gone: %+v, want it unreachable, saying why", tc.lib.Root, g)
		}
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, other.URL+"/api/v1/internal/libraries/"+readable.ID.String()+"/check", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a client asking a node to check a root: %s, want 401", resp.Status)
	}
}

// Checks of a root whose read hangs, as on a hung mount, share the one read: each gives up by its
// own deadline, and only once that read ends does a later check read the root again.
func TestChecksOfAHungRootShareOneRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var started atomic.Int32
		release := make(chan struct{})
		reads := &rootReads{read: func(string) rootCheckJSON {
			started.Add(1)
			<-release
			return rootCheckJSON{Access: accessReadable}
		}}
		var wg sync.WaitGroup
		for _, within := range []time.Duration{time.Second, 2 * time.Second} {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(t.Context(), within)
				defer cancel()
				if got := reads.check(ctx, "/mnt/hung"); got.Access != accessTimedOut {
					t.Errorf("a check of a hung root: %+v, want it timed out", got)
				}
			})
		}
		wg.Wait()
		if n := started.Load(); n != 1 {
			t.Errorf("two checks of a hung root started %d reads, want 1", n)
		}
		close(release)
		synctest.Wait()
		if got := reads.check(t.Context(), "/mnt/hung"); got.Access != accessReadable || started.Load() != 2 {
			t.Errorf("a check once the mount answers: %+v after %d reads, want a fresh read of it", got, started.Load())
		}
	})
}
