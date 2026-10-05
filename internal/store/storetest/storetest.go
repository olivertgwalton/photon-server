// Package storetest gives each integration test a database of its own.
package storetest

import (
	"context"
	"net/url"
	"os"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// FreshDatabase creates an empty database on the server TEST_DATABASE_URL names and drops it when
// the test ends.
func FreshDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("TEST_DATABASE_URL is not set")
	}
	admin, err := pgx.Connect(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	// t.Context is already cancelled when cleanups run.
	t.Cleanup(func() { admin.Close(context.Background()) })

	name := "photon_" + uuid.NewV4().String()[:8]
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
