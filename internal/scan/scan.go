package scan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type prober interface {
	Probe(ctx context.Context, f *os.File) (media.Facts, error)
}

type Scanner struct {
	store  *store.Store
	prober prober
	log    *slog.Logger
}

func New(st *store.Store, p prober, log *slog.Logger) *Scanner {
	return &Scanner{store: st, prober: p, log: log}
}

// Report counts what a scan did; Probed is zero for a library nothing in has changed.
type Report struct {
	Folders   int
	Unchanged int
	Probed    int
	Skipped   int
}

var errShowsNotScanned = errors.New("shows libraries cannot be scanned yet")

func (s *Scanner) Scan(ctx context.Context, lib domain.Library) (Report, error) {
	switch lib.Kind {
	case domain.LibraryMovies:
	case domain.LibraryShows:
		return Report{}, errShowsNotScanned
	}
	root, err := os.OpenRoot(lib.Root)
	if err != nil {
		return Report{}, err
	}
	defer root.Close()

	var report Report
	var folders, present []string
	claimed := map[string]string{}
	for folder, err := range library.Walk(root) {
		if err != nil {
			s.log.WarnContext(ctx, "folder not read", slog.String("folder", folder.Path), slog.Any("err", err))
			continue
		}
		report.Folders++
		folders = append(folders, folder.Path)
		for _, f := range folder.Files {
			if naming.IsVideo(f.Name) {
				present = append(present, path.Join(folder.Path, f.Name))
			}
		}
		known, err := s.store.FolderFingerprint(ctx, lib.ID, folder.Path)
		if err != nil {
			return report, err
		}
		if bytes.Equal(known, folder.Fingerprint[:]) {
			report.Unchanged++
			continue
		}
		films, err := s.films(ctx, root, lib, folder, claimed, &report)
		if err != nil {
			return report, err
		}
		if err := s.store.SaveFolder(ctx, lib.ID, folder.Path, folder.Fingerprint[:], films); err != nil {
			return report, fmt.Errorf("%s: %w", folder.Path, err)
		}
	}
	return report, s.store.FinishScan(ctx, lib.ID, folders, present)
}

// films turns a folder's plan into what the store keeps, reading each copy's content key and
// probing only copies the catalogue does not hold. A copy that cannot be read is left out and
// logged; claimed keeps a byte-identical copy in a second place from becoming a second version.
func (s *Scanner) films(ctx context.Context, root *os.Root, lib domain.Library, folder library.Folder, claimed map[string]string, report *Report) ([]store.Film, error) {
	var films []store.Film
	for _, f := range planFilms(folder) {
		film := store.Film{Title: f.name.Title, Year: f.name.Year, Folder: folder.Path, IDs: ids(f.name.IDs)}
		for _, v := range f.versions {
			c, ok, err := s.copy(ctx, root, lib, folder.Path, v, claimed, report)
			if err != nil {
				return nil, err
			}
			if ok {
				film.Copies = append(film.Copies, c)
			}
		}
		if len(film.Copies) > 0 {
			films = append(films, film)
		}
	}
	return films, nil
}

func (s *Scanner) copy(ctx context.Context, root *os.Root, lib domain.Library, dir string, v copyPlan, claimed map[string]string, report *Report) (store.Copy, bool, error) {
	c := store.Copy{Edition: v.edition, Label: v.label}
	paths := make([]string, len(v.parts))
	for i, p := range v.parts {
		paths[i] = path.Join(dir, p.Name)
		c.Parts = append(c.Parts, store.Part{RelPath: paths[i], Size: p.Size, ModTime: p.ModTime})
	}
	key, err := library.ContentKey(root, paths)
	if err != nil {
		s.skip(ctx, report, paths[0], err)
		return c, false, nil
	}
	if first, dup := claimed[string(key)]; dup {
		s.skip(ctx, report, paths[0], fmt.Errorf("the same file as %s", first))
		return c, false, nil
	}
	claimed[string(key)] = paths[0]
	c.ContentKey = key
	known, err := s.store.KnownCopy(ctx, lib.ID, key)
	if err != nil {
		return c, false, err
	}
	if known {
		return c, true, nil
	}
	for i := range c.Parts {
		facts, err := s.probe(ctx, root, paths[i])
		report.Probed++
		if err != nil {
			s.skip(ctx, report, paths[i], err)
			return c, false, nil
		}
		c.Parts[i].Facts = &facts
	}
	return c, true, nil
}

func (s *Scanner) probe(ctx context.Context, root *os.Root, rel string) (media.Facts, error) {
	f, err := root.Open(rel)
	if err != nil {
		return media.Facts{}, err
	}
	defer f.Close()
	return s.prober.Probe(ctx, f)
}

func (s *Scanner) skip(ctx context.Context, report *Report, rel string, err error) {
	report.Skipped++
	s.log.WarnContext(ctx, "file left out", slog.String("file", rel), slog.Any("err", err))
}

func ids(n naming.IDs) map[domain.Provider]string {
	out := map[domain.Provider]string{}
	for p, v := range map[domain.Provider]string{
		domain.ProviderTMDB: n.TMDB, domain.ProviderIMDb: n.IMDb, domain.ProviderTVDB: n.TVDB,
	} {
		if v != "" {
			out[p] = v
		}
	}
	return out
}
