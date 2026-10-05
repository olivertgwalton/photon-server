// Package backup dumps the database on a schedule, as Plex backs its up every three days, so a
// server can be put back as it was with pg_restore.
package backup

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Keep is how many dumps are kept, the newest.
const Keep = 3

const prefix, suffix = "photon-", ".dump"

// Dumper writes dumps of a database into a folder with pg_dump.
type Dumper struct {
	PGDump string
	URL    string
	Dir    string
}

// Dump writes a dump in pg_dump's custom format, whole or not at all, and removes all but the
// newest Keep. The password goes to pg_dump in its environment, not its arguments, where anyone
// listing processes would see it.
func (d Dumper) Dump(ctx context.Context, now time.Time) (string, error) {
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return "", err
	}
	u, err := url.Parse(d.URL)
	if err != nil {
		return "", fmt.Errorf("database url: %w", err)
	}
	env := os.Environ()
	if pw, ok := u.User.Password(); ok {
		env = append(env, "PGPASSWORD="+pw)
		u.User = url.User(u.User.Username())
	}
	name := filepath.Join(d.Dir, prefix+now.UTC().Format("20060102T150405Z")+suffix)
	part := name + ".part"
	cmd := exec.CommandContext(ctx, d.PGDump, "--format=custom", "--no-owner", "--file="+part, "--dbname="+u.String()) //nolint:gosec // the configured pg_dump; every argument is built here
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("pg_dump: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if err := os.Rename(part, name); err != nil {
		return "", err
	}
	return name, d.prune()
}

// prune removes all but the newest Keep dumps; their names sort by when they were made.
func (d Dumper) prune() error {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return err
	}
	var dumps []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), suffix) {
			dumps = append(dumps, e.Name())
		}
	}
	slices.Sort(dumps)
	for _, old := range dumps[:max(len(dumps)-Keep, 0)] {
		if err := os.Remove(filepath.Join(d.Dir, old)); err != nil {
			return err
		}
	}
	return nil
}
