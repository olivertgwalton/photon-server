package scan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/nfo"
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

func (s *Scanner) Scan(ctx context.Context, lib domain.Library) (Report, error) {
	root, err := os.OpenRoot(lib.Root)
	if err != nil {
		return Report{}, err
	}
	defer root.Close()

	var report Report
	var folders, present []string
	for folder, err := range library.Walk(root) {
		if err != nil {
			s.log.WarnContext(ctx, "folder not read", slog.String("folder", folder.Path), slog.Any("err", err))
			continue
		}
		report.Folders++
		folders = append(folders, folder.Path)
		for _, f := range folder.Files {
			if naming.IsVideo(f.Name) || naming.IsSubtitle(f.Name) {
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
		switch lib.Kind {
		case domain.LibraryMovies:
			err = s.saveFilms(ctx, root, lib, folder, &report)
		case domain.LibraryShows:
			err = s.saveEpisodes(ctx, root, lib, folder, &report)
		}
		if err != nil {
			return report, fmt.Errorf("%s: %w", folder.Path, err)
		}
	}
	return report, s.store.FinishScan(ctx, lib.ID, folders, present)
}

func (s *Scanner) saveFilms(ctx context.Context, root *os.Root, lib domain.Library, folder library.Folder, report *Report) error {
	var films []store.Film
	for _, f := range planFilms(folder) {
		copies, err := s.copies(ctx, root, lib, folder.Path, f.versions, report)
		if err != nil {
			return err
		}
		if len(copies) > 0 {
			films = append(films, store.Film{
				Title: f.name.Title, Year: f.name.Year, Folder: folder.Path, IDs: ids(f.name.IDs),
				NFO: s.readNFO(ctx, root, folder.Path, f.nfos...), Copies: copies,
			})
		}
	}
	plans, inExtrasFolder := extrasIn(folder)
	extras, err := s.extras(ctx, root, lib, folder, plans, report, func(e extraPlan) store.Owner {
		if inExtrasFolder {
			return store.Owner{Kind: domain.ItemMovie, Folder: path.Dir(folder.Path)}
		}
		return store.Owner{Kind: domain.ItemMovie, Folder: folder.Path, Title: filmNamed(e.ownerName, films)}
	})
	if err != nil {
		return err
	}
	unowned, err := s.store.SaveFolder(ctx, lib.ID, folder.Path, folder.Fingerprint[:], films, extras)
	s.unowned(ctx, report, unowned)
	return err
}

// filmNamed is the title of the film an extra's name gives: the folder's only film, else the one
// whose copy the name matches, else the title the name reads as.
func filmNamed(name string, films []store.Film) string {
	if len(films) == 1 {
		return films[0].Title
	}
	for _, f := range films {
		for _, c := range f.Copies {
			if strings.EqualFold(stem(path.Base(c.Parts[0].RelPath)), name) {
				return f.Title
			}
		}
	}
	return naming.CleanName(name).Title
}

// extras reads each planned extra's copy, probing it if new, and names its owner.
func (s *Scanner) extras(ctx context.Context, root *os.Root, lib domain.Library, folder library.Folder, plans []extraPlan, report *Report, owner func(extraPlan) store.Owner) ([]store.Extra, error) {
	var extras []store.Extra
	for _, e := range plans {
		c, ok, err := s.copy(ctx, root, lib, folder.Path, e.copy, report)
		if err != nil {
			return nil, err
		}
		if ok {
			extras = append(extras, store.Extra{Kind: e.kind, Title: e.title, Folder: folder.Path, Owner: owner(e), Copy: c})
		}
	}
	return extras, nil
}

var errNoOwner = errors.New("no single title it could belong to")

func (s *Scanner) unowned(ctx context.Context, report *Report, paths []string) {
	for _, p := range paths {
		s.skip(ctx, report, p, errNoOwner)
	}
}

func (s *Scanner) saveEpisodes(ctx context.Context, root *os.Root, lib domain.Library, folder library.Folder, report *Report) error {
	series, season := seriesOf(folder.Path)
	var show store.Show
	if series != "" {
		name := naming.SeriesName(series)
		show = store.Show{
			Title: name.Title, Year: name.Year, Folder: series, IDs: ids(name.IDs),
			NFO: s.readNFO(ctx, root, series, "tvshow.nfo"),
		}
	}
	var episodes []store.Episode
	if holdsEpisodes(folder.Path) {
		plans, unread := planEpisodes(folder, season, show.Title)
		for _, rel := range unread {
			s.skip(ctx, report, rel, errNoEpisode)
		}
		for _, e := range plans {
			copies, err := s.copies(ctx, root, lib, folder.Path, e.versions, report)
			if err != nil {
				return err
			}
			if len(copies) > 0 {
				episodes = append(episodes, store.Episode{
					Season: e.season, Episodes: e.episodes, AirDate: e.airDate, Title: e.title,
					Folder: folder.Path, IDs: ids(e.name.IDs), ByNumber: e.byNumber, Copies: copies,
					NFO: s.readNFO(ctx, root, folder.Path, nfoOf(e.versions[0].parts)),
				})
			}
		}
	}
	plans, inExtrasFolder := extrasIn(folder)
	extras, err := s.extras(ctx, root, lib, folder, plans, report, func(e extraPlan) store.Owner {
		o := store.Owner{Kind: domain.ItemShow, Folder: series}
		if season != nil {
			o.Kind, o.Season = domain.ItemSeason, *season
		}
		if ep, ok := naming.ParseEpisode(e.ownerName, ""); !inExtrasFolder && ok && ep.Confidence == naming.ConfidenceHigh && len(ep.Episodes) > 0 {
			o.Kind, o.Episode = domain.ItemEpisode, ep.Episodes[0]
			switch {
			case ep.Season != nil:
				o.Season = *ep.Season
			case season != nil:
				o.Season = *season
			default:
				o.Season = 1
			}
		}
		return o
	})
	if err != nil {
		return err
	}
	unowned, err := s.store.SaveShowFolder(ctx, lib.ID, folder.Path, folder.Fingerprint[:], show, episodes, extras)
	s.unowned(ctx, report, unowned)
	return err
}

var errNoEpisode = errors.New("its name says no season or episode")

// copies reads each copy's content key and probes only copies the catalogue does not hold. A copy
// that cannot be read is left out and logged. A byte-identical copy in a second place is a known
// copy: it becomes another place to read the same version.
func (s *Scanner) copies(ctx context.Context, root *os.Root, lib domain.Library, dir string, plans []copyPlan, report *Report) ([]store.Copy, error) {
	var copies []store.Copy
	for _, v := range plans {
		c, ok, err := s.copy(ctx, root, lib, dir, v, report)
		if err != nil {
			return nil, err
		}
		if ok {
			copies = append(copies, c)
		}
	}
	return copies, nil
}

func (s *Scanner) copy(ctx context.Context, root *os.Root, lib domain.Library, dir string, v copyPlan, report *Report) (store.Copy, bool, error) {
	c := store.Copy{Edition: v.edition, Label: v.label}
	for _, sub := range v.subtitles {
		c.Subtitles = append(c.Subtitles, store.Subtitle{
			RelPath: path.Join(dir, sub.file.Name), Size: sub.file.Size, ModTime: sub.file.ModTime,
			Codec: sub.codec, Language: sub.tags.Language, Title: sub.tags.Title,
			Forced: sub.tags.Forced, Default: sub.tags.Default, HearingImpaired: sub.tags.HearingImpaired,
		})
	}
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

// readNFO reads the first of the named NFOs in dir that exists. One that cannot be read is
// logged and the title goes on without it.
func (s *Scanner) readNFO(ctx context.Context, root *os.Root, dir string, names ...string) *domain.Metadata {
	for _, name := range names {
		rel := path.Join(dir, name)
		f, err := root.Open(rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			var m domain.Metadata
			m, err = nfo.Read(f)
			f.Close()
			if err == nil {
				return &m
			}
		}
		s.log.WarnContext(ctx, "NFO not read", slog.String("file", rel), slog.Any("err", err))
		return nil
	}
	return nil
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
