//go:build integration

package store

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func migrated(t *testing.T) *Store {
	t.Helper()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestOpenRefusesUnmigratedDatabase(t *testing.T) {
	_, err := Open(t.Context(), storetest.FreshDatabase(t), slog.New(slog.DiscardHandler))
	if !errors.Is(err, errSchema) {
		t.Fatalf("Open on an empty database: err = %v, want %v", err, errSchema)
	}
}

func TestServerIDSurvivesReopening(t *testing.T) {
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 2)
	for i := range ids {
		s, err := Open(t.Context(), db, log)
		if err != nil {
			t.Fatal(err)
		}
		ids[i], err = s.ServerID(t.Context())
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] == (uuid.UUID{}) || ids[0] != ids[1] {
		t.Errorf("server ids = %v, want one stable non-zero id", ids)
	}
}

func TestARunningNodeIsListedAmongTheDatabasesConnections(t *testing.T) {
	db := storetest.FreshDatabase(t)
	if others, err := Connections(t.Context(), db); err != nil || len(others) != 0 {
		t.Fatalf("a database no one uses: %v, %v; want no one", others, err)
	}
	log := slog.New(slog.DiscardHandler)
	if err := Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host, _ := os.Hostname()
	others, err := Connections(t.Context(), db)
	if err != nil || len(others) != 1 || !strings.HasPrefix(others[0], "photon-server "+host+" from ") {
		t.Errorf("with a node running: %q, %v; want it, named by its host", others, err)
	}
}

// A node starting asks for the server's id before it opens the database, to ask Valkey whether a
// restore is under way, and is not held up by one holding the database.
func TestANodeStartingIsToldTheDatabaseIsHeldByARestore(t *testing.T) {
	db := storetest.FreshDatabase(t)
	if id, err := WaitingServerID(t.Context(), db); err != nil || id != (uuid.UUID{}) {
		t.Fatalf("a database not yet migrated: %v %v, want no id", id, err)
	}
	log := slog.New(slog.DiscardHandler)
	if err := Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.ServerID(t.Context())
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	if id, err := WaitingServerID(t.Context(), db); err != nil || id != want {
		t.Errorf("a migrated database: %v %v, want %v", id, err, want)
	}
	restore, err := pgx.Connect(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer restore.Close(context.Background())
	if _, err := restore.Exec(t.Context(), "BEGIN; LOCK TABLE server IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	if _, err := WaitingServerID(t.Context(), db); !errors.Is(err, ErrHeld) {
		t.Errorf("while a restore holds it: %v, want ErrHeld", err)
	}
}

// addItem writes a title as a scan would, answering its id; a zero ID, AddedAt or EpisodeOrder takes
// the column's default.
func addItem(t *testing.T, s *Store, i model.Item) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := s.pool.QueryRow(t.Context(), `
		INSERT INTO items (id, library_id, kind, title, sort_title, year, folder, added_at, parent_id, season_number,
			episode_number, episode_end, air_date, extra_kind, scan_title, original_title, overview, tagline,
			certificate, release_date, genres, studios, episode_order)
		VALUES (coalesce($1, uuidv7()), $2, $3, $4, $5, $6, $7, coalesce($8, now()), $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22, coalesce(nullif($23, ''), 'aired'))
		RETURNING id`,
		zeroNull(i.ID), i.LibraryID, i.Kind, i.Title, i.SortTitle, i.Year, i.Folder, zeroNull(i.AddedAt), i.ParentID,
		i.SeasonNumber, i.EpisodeNumber, i.EpisodeEnd, i.AirDate, i.ExtraKind, i.ScanTitle, i.OriginalTitle, i.Overview,
		i.Tagline, i.Certificate, i.ReleaseDate, i.Genres, i.Studios, i.EpisodeOrder).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// oneItem answers an item a condition finds.
func oneItem(t *testing.T, s *Store, where string, args ...any) *model.Item {
	t.Helper()
	rows, err := queryRows[model.Item](t.Context(), s.pool, `SELECT `+itemColumns+` FROM items WHERE `+where+` LIMIT 1`, args...)
	if err != nil || len(rows) == 0 {
		t.Fatalf("no item where %s: %v", where, err)
	}
	return rows[0]
}

// countRows answers how many rows a query counts.
func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// zeroNull is nil for a zero value, so its column takes its default.
func zeroNull[T comparable](v T) *T {
	if v == *new(T) {
		return nil
	}
	return &v
}

// Every foreign key leads an index, or removing what it points at scans the whole table for
// each row removed.
func TestEveryForeignKeyIsIndexed(t *testing.T) {
	s := migrated(t)
	rows, err := s.pool.Query(t.Context(), `
		SELECT c.conname FROM pg_constraint c
		WHERE c.contype = 'f' AND NOT EXISTS (
			SELECT 1 FROM pg_index i WHERE i.indrelid = c.conrelid
				AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] @> c.conkey
				AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] <@ c.conkey)`)
	if err != nil {
		t.Fatal(err)
	}
	unindexed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(unindexed) > 0 {
		t.Errorf("foreign keys with no index: %q, %v", unindexed, err)
	}
}
