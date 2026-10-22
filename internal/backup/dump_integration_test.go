//go:build integration

package backup

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// A dump of a migrated database restores whole, with PostgreSQL's own tools: the functions that
// call each other among it, which pg_dump cannot order where both bodies are read as they are made,
// and what a profile sees through them.
func TestADumpOfTheDatabaseRestores(t *testing.T) {
	tools := map[string]string{}
	for _, name := range []string{"pg_dump", "pg_restore", "psql"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not on PATH", name)
		}
		tools[name] = path
	}
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	open := func(url string) *store.Store {
		t.Helper()
		st, err := store.Open(ctx, url, log)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	from := storetest.FreshDatabase(t)
	if err := store.Migrate(ctx, from, log); err != nil {
		t.Fatal(err)
	}
	st := open(from)
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	// An empty collection is seen through sees and sees_collection alike.
	heists, err := st.AddCollection(ctx, "Heists")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	file, err := Dumper{PGDump: tools["pg_dump"], URL: from, Dir: t.TempDir()}.Dump(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	to := storetest.FreshDatabase(t)
	r := Restorer{PGRestore: tools["pg_restore"], PSQL: tools["psql"], DatabaseURL: to, ValkeyURL: os.Getenv("TEST_VALKEY_URL"), Log: log}
	if err := r.Restore(ctx, file, io.Discard); err != nil {
		t.Fatalf("restoring the dump: %v", err)
	}
	st = open(to)
	defer st.Close()
	seen, err := st.Visible(ctx, ada.ID, []uuid.UUID{heists})
	if err != nil || !slices.Equal(seen, []uuid.UUID{heists}) {
		t.Errorf("Ada sees %v, %v of the restored database; want its collection", seen, err)
	}
}
