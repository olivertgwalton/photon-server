//go:build integration

package store

import (
	"errors"
	"log/slog"
	"testing"
	"uuid"

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
