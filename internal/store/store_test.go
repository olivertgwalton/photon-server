//go:build integration

package store

import (
	"errors"
	"log/slog"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"

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

// Every model field must have a column of the same name in the migrated schema.
func TestModelsMatchSchema(t *testing.T) {
	s := migrated(t)
	db := s.q.Server.WithContext(t.Context()).UnderlyingDB().Session(&gorm.Session{NewDB: true})
	for _, m := range model.All() {
		stmt := db.Model(m).Statement
		if err := stmt.Parse(m); err != nil {
			t.Fatal(err)
		}
		cols, err := db.Migrator().ColumnTypes(m)
		if err != nil {
			t.Fatal(err)
		}
		if len(cols) == 0 {
			t.Errorf("%s: table %s does not exist", stmt.Schema.Name, stmt.Schema.Table)
		}
		have := map[string]bool{}
		for _, c := range cols {
			have[c.Name()] = true
		}
		for _, f := range stmt.Schema.Fields {
			if f.DBName != "" && !have[f.DBName] {
				t.Errorf("%s.%s: no column %q in table %s", stmt.Schema.Name, f.Name, f.DBName, stmt.Schema.Table)
			}
		}
	}
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
