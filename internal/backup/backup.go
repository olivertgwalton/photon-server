// Package backup dumps the database on a schedule, as Plex backs its up every three days, so a
// server can be put back as it was with pg_restore.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/media"
)

// Keep is how many dumps are kept, the newest.
const Keep = 3

const prefix, stamp, suffix = "photon-", "20060102T150405Z", ".dump"

// madeAt answers when the dump named name was made, or false for a name no dump is given: what
// keeps a name from a request from reaching outside the folder.
func madeAt(name string) (time.Time, bool) {
	s, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return time.Time{}, false
	}
	if s, ok = strings.CutSuffix(s, suffix); !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(stamp, s)
	return t, err == nil && t.Format(stamp) == s
}

// Dump is a dump kept in the folder.
type Dump struct {
	Name   string
	Size   int64
	MadeAt time.Time
}

// List answers the dumps in dir, the newest first; none where there is no dir yet.
func List(dir string) ([]Dump, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dumps []Dump
	for _, e := range entries {
		at, ok := madeAt(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		dumps = append(dumps, Dump{Name: e.Name(), Size: info.Size(), MadeAt: at})
	}
	slices.Reverse(dumps)
	return dumps, nil
}

// Open opens the dump in dir named name; fs.ErrNotExist for a name no dump has.
func Open(dir, name string) (*os.File, error) {
	if _, ok := madeAt(name); !ok {
		return nil, fs.ErrNotExist
	}
	return os.Open(filepath.Join(dir, name))
}

// Dumper writes dumps of a database into a folder with pg_dump, found by media.Look.
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
	dbURL, env, err := withoutPassword(d.URL)
	if err != nil {
		return "", err
	}
	name := filepath.Join(d.Dir, prefix+now.UTC().Format(stamp)+suffix)
	part := name + ".part"
	cmd := media.NewCommand(ctx, media.Background, nil, d.PGDump, "--format=custom", "--no-owner", "--file="+part, "--dbname="+dbURL)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		// RemoveAll, as pg_dump may have failed before writing anything.
		return "", errors.Join(cmd.Err(err), os.RemoveAll(part))
	}
	if err := os.Rename(part, name); err != nil {
		return "", err
	}
	return name, d.prune()
}

// withoutPassword answers the database URL without its password, and this process's environment
// with it as PGPASSWORD, where anyone listing processes would not see it.
func withoutPassword(databaseURL string) (string, []string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", nil, fmt.Errorf("database url: %w", err)
	}
	env := os.Environ()
	if pw, ok := u.User.Password(); ok {
		env = append(env, "PGPASSWORD="+pw)
		u.User = url.User(u.User.Username())
	}
	return u.String(), env, nil
}

// prune removes all but the newest Keep dumps; their names sort by when they were made.
func (d Dumper) prune() error {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return err
	}
	var dumps []string
	for _, e := range entries {
		if _, ok := madeAt(e.Name()); ok {
			dumps = append(dumps, e.Name())
		}
	}
	for _, old := range dumps[:max(len(dumps)-Keep, 0)] {
		if err := os.Remove(filepath.Join(d.Dir, old)); err != nil {
			return err
		}
	}
	return nil
}
