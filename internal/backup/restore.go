package backup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Restorer puts the database back as a dump has it, with every node stopped.
type Restorer struct {
	PGRestore   string
	PSQL        string
	DatabaseURL string
	ValkeyURL   string
	Log         *slog.Logger
}

// Restore replaces the database with the dump in file and migrates it to this binary's schema,
// then clears the server's keys in Valkey, which may name playbacks, nodes and pairings the dump
// never had. It refuses, changing nothing, a dump newer than this binary, and a database anyone
// else is connected to: a node running would write over what is restored. What it did is
// written to out.
func (r Restorer) Restore(ctx context.Context, file string, out io.Writer) error {
	have, want, err := r.check(ctx, file)
	if err != nil {
		return err
	}
	others, err := store.Connections(ctx, r.DatabaseURL)
	if err != nil {
		return err
	}
	if len(others) > 0 {
		return fmt.Errorf("stop every photon-server, and anything else using the database, before restoring; connected now:\n  %s", strings.Join(others, "\n  "))
	}
	probe, err := kv.Open(r.ValkeyURL, uuid.UUID{})
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	err = probe.Ping(ctx)
	probe.Close()
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}

	if err := r.replace(ctx, file); err != nil {
		return err
	}
	fmt.Fprintf(out, "Restored %s, made at schema version %d.\n", file, have)
	if err := store.Migrate(ctx, r.DatabaseURL, r.Log); err != nil {
		return err
	}
	if have < want {
		fmt.Fprintf(out, "Migrated it to schema version %d.\n", want)
	}
	st, err := store.Open(ctx, r.DatabaseURL, r.Log)
	if err != nil {
		return err
	}
	id, err := st.ServerID(ctx)
	st.Close()
	if err != nil {
		return err
	}
	cache, err := kv.Open(r.ValkeyURL, id)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	n, err := cache.Clear(ctx)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	fmt.Fprintf(out, "Cleared %d of the server's keys in Valkey: playbacks, nodes, pairings and counts.\n", n)
	err = cache.SaveRestoreOutcome(ctx, domain.RestoreOutcome{Dump: filepath.Base(file), At: time.Now().UTC(), Result: domain.RestoreSucceeded})
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	fmt.Fprintln(out, "Cached artwork, previews and transcodes are left; each node's sweeps forget what the database no longer has.")
	return nil
}

// ErrNewer is a dump made by a newer photon-server than this one, whose schema it does not know.
var ErrNewer = errors.New("restore it with the version that made it, or a newer one")

// check answers the schema version file was made at and this binary's, refusing a newer dump.
func (r Restorer) check(ctx context.Context, file string) (have, want int64, err error) {
	if have, err = r.version(ctx, file); err != nil {
		return 0, 0, err
	}
	if want, err = store.SchemaVersion(); err != nil {
		return 0, 0, err
	}
	if have > want {
		return 0, 0, fmt.Errorf("%s is at schema version %d, newer than this photon-server's %d: %w", filepath.Base(file), have, want, ErrNewer)
	}
	return have, want, nil
}

// version reads the schema version a dump was made at from its goose_db_version table, the
// newest migration applied there, before anything is restored.
func (r Restorer) version(ctx context.Context, file string) (int64, error) {
	cmd := exec.CommandContext(ctx, r.PGRestore, "--data-only", "--table=goose_db_version", "--file=-", file) //nolint:gosec // the configured pg_restore; the file is the operator's
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("pg_restore: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return appliedVersion(&stdout)
}

// appliedVersion reads the newest version applied from the COPY of goose_db_version in a script
// pg_restore writes.
func appliedVersion(script io.Reader) (int64, error) {
	lines := bufio.NewScanner(script)
	var version, applied int
	copying := false
	newest := int64(-1)
	for lines.Scan() {
		line := lines.Text()
		if !copying {
			cols, ok := strings.CutPrefix(line, "COPY public.goose_db_version (")
			if !ok {
				continue
			}
			cols, _, _ = strings.Cut(cols, ")")
			names := strings.Split(cols, ", ")
			version, applied = slices.Index(names, "version_id"), slices.Index(names, "is_applied")
			if version < 0 || applied < 0 {
				return 0, fmt.Errorf("goose_db_version has columns %s", cols)
			}
			copying = true
			continue
		}
		if line == `\.` {
			break
		}
		fields := strings.Split(line, "\t")
		if len(fields) <= max(version, applied) || fields[applied] != "t" {
			continue
		}
		v, err := strconv.ParseInt(fields[version], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("goose_db_version: %w", err)
		}
		newest = max(newest, v)
	}
	if err := lines.Err(); err != nil {
		return 0, err
	}
	if newest < 0 {
		return 0, errors.New("the dump has no migrations recorded: it is not of photon-server's database")
	}
	return newest, nil
}

// replace drops the public schema and restores the dump into it in one transaction, committed
// only once pg_restore has written the whole dump: a restore that fails leaves the database as it
// was, and none of a newer schema's tables outlive it to trip the migrations that make them again.
func (r Restorer) replace(ctx context.Context, file string) error {
	dbURL, env, err := withoutPassword(r.DatabaseURL)
	if err != nil {
		return err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer write.Close()
	var psqlErr, dumpErr bytes.Buffer
	psql := exec.CommandContext(ctx, r.PSQL, "--no-psqlrc", "--quiet", "--set=ON_ERROR_STOP=1", "--dbname="+dbURL) //nolint:gosec // the configured psql; every argument is built here
	psql.Env, psql.Stdin, psql.Stderr = env, read, &psqlErr
	err = psql.Start()
	_ = read.Close()
	if err != nil {
		return fmt.Errorf("psql: %w", err)
	}
	dump := exec.CommandContext(ctx, r.PGRestore, "--no-owner", "--no-privileges", "--file=-", file) //nolint:gosec // the configured pg_restore; the file is the operator's
	dump.Stdout, dump.Stderr = write, &dumpErr
	_, err = io.WriteString(write, "BEGIN;\nSET client_min_messages = warning;\nDROP SCHEMA IF EXISTS public CASCADE;\nCREATE SCHEMA public;\n")
	if err == nil {
		err = dump.Run()
	}
	if err == nil {
		_, err = io.WriteString(write, "COMMIT;\n")
	}
	// Without COMMIT, psql reaches the end of its input and Postgres rolls the transaction back.
	_ = write.Close()
	if waitErr := psql.Wait(); waitErr != nil {
		return fmt.Errorf("psql: %w: %s", waitErr, bytes.TrimSpace(psqlErr.Bytes()))
	}
	if err != nil {
		return fmt.Errorf("pg_restore: %w: %s", err, bytes.TrimSpace(dumpErr.Bytes()))
	}
	return nil
}
