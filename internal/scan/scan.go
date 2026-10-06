package scan

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"slices"
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

// Scan reads a library's folders again, telling progress after each, and what each changed of the
// library's titles.
func (s *Scanner) Scan(ctx context.Context, lib domain.Library, progress func(domain.ScanProgress), changed func(store.Changed)) (Report, error) {
	var report Report
	var folders, present []string
	// The root is known before it is read; each folder read makes its subfolders known.
	told := domain.ScanProgress{Library: lib.ID, Phase: domain.ScanReading, Known: 1}
	for folder, err := range library.Walk(lib.Root) {
		told.Done++
		told.Known += len(folder.Folders)
		// A root that cannot be read is a mount that is down, not a library emptied.
		if err != nil && folder.Path == "." {
			return report, err
		}
		if err != nil {
			s.log.WarnContext(ctx, "folder not read", slog.String("folder", folder.Path), slog.Any("err", err))
			progress(told)
			continue
		}
		report.Folders++
		folders = append(folders, folder.Path)
		for _, sk := range folder.Skipped {
			s.skip(ctx, &report, path.Join(folder.Path, sk.Name), sk.Err)
		}
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
			progress(told)
			continue
		}
		var saved store.Saved
		switch lib.Kind {
		case domain.LibraryMovies:
			saved, err = s.saveFilms(ctx, lib, folder, &report)
		case domain.LibraryShows:
			saved, err = s.saveEpisodes(ctx, lib, folder, &report)
		}
		if err != nil {
			return report, fmt.Errorf("%s: %w", folder.Path, err)
		}
		s.unowned(ctx, &report, saved.Unowned)
		changed(saved.Titles)
		progress(told)
	}
	// A folder an empty .ignore hides was known and never read.
	told.Phase, told.Known = domain.ScanRemoving, told.Done
	progress(told)
	titles, err := s.store.FinishScan(ctx, lib.ID, folders, present)
	if err == nil {
		changed(titles)
	}
	return report, err
}

func (s *Scanner) saveFilms(ctx context.Context, lib domain.Library, folder library.Folder, report *Report) (store.Saved, error) {
	var films []store.Film
	plans := planFilms(folder)
	pics := picturesIn(folder.Path, fileNames(folder))
	for _, f := range plans {
		copies, err := s.copies(ctx, lib, folder.Path, f.versions, report)
		if err != nil {
			return store.Saved{}, err
		}
		if len(copies) == 0 {
			continue
		}
		var art []domain.Artwork
		for _, v := range f.versions {
			art = append(art, pics.of(stem(v.parts[0].Name), domain.ArtworkPoster)...)
		}
		// A folder's own pictures are its film's when it holds one, as Jellyfin reads them.
		if len(plans) == 1 && folder.Path != "." {
			art = append(art, pics.own...)
		}
		films = append(films, store.Film{
			Title: f.name.Title, Year: f.name.Year, Folder: folder.Path, IDs: ids(f.name.IDs),
			NFO: metadata(s.readNFO(ctx, lib, folder.Path, f.nfos...)), Artwork: art, Copies: copies,
		})
	}
	extraPlans, inExtrasFolder := extrasIn(folder)
	extras, err := s.extras(ctx, lib, folder, extraPlans, report, func(e extraPlan) store.Owner {
		if inExtrasFolder {
			return store.Owner{Kind: domain.ItemMovie, Folder: path.Dir(folder.Path)}
		}
		return store.Owner{Kind: domain.ItemMovie, Folder: folder.Path, Title: filmNamed(e.ownerName, films)}
	})
	if err != nil {
		return store.Saved{}, err
	}
	return s.store.SaveFolder(ctx, lib.ID, folder.Path, folder.Fingerprint[:], films, extras)
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
func (s *Scanner) extras(ctx context.Context, lib domain.Library, folder library.Folder, plans []extraPlan, report *Report, owner func(extraPlan) store.Owner) ([]store.Extra, error) {
	var extras []store.Extra
	for _, e := range plans {
		c, ok, err := s.copy(ctx, lib, folder.Path, e.copy, report)
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

func (s *Scanner) saveEpisodes(ctx context.Context, lib domain.Library, folder library.Folder, report *Report) (store.Saved, error) {
	series, season := seriesOf(folder.Path)
	var show store.Show
	if series != "" {
		name := naming.SeriesName(series)
		said := s.readNFO(ctx, lib, series, "tvshow.nfo")
		show = store.Show{
			Title: name.Title, Year: name.Year, Folder: series, IDs: ids(name.IDs), NFO: metadata(said),
			Seasons: map[int]domain.Metadata{},
		}
		if said != nil {
			for n, title := range said.SeasonNames {
				show.Seasons[n] = domain.Metadata{Title: title}
			}
		}
		// Jellyfin reads season.nfo only in the season's own folder.
		if said := s.readNFO(ctx, lib, folder.Path, "season.nfo"); said != nil && season != nil {
			n := cmp.Or(said.Season, season)
			said.Title = cmp.Or(said.Title, show.Seasons[*n].Title)
			show.Seasons[*n] = said.Metadata
		}
	}
	pics := picturesIn(folder.Path, fileNames(folder))
	switch {
	case series != "" && folder.Path == series:
		show.Artwork = append([]domain.Artwork{}, pics.own...)
		show.SeasonArtwork = pics.seasons
	case series != "" && season != nil:
		show.SeasonArtwork = map[int][]domain.Artwork{
			*season: append(pics.own, seasonPictures(lib.Root, series, *season)...),
		}
	}
	var episodes []store.Episode
	if holdsEpisodes(folder.Path) {
		plans, unread := planEpisodes(folder, season, show.Title)
		for _, rel := range unread {
			s.skip(ctx, report, rel, errNoEpisode)
		}
		for _, e := range plans {
			copies, err := s.copies(ctx, lib, folder.Path, e.versions, report)
			if err != nil {
				return store.Saved{}, err
			}
			if len(copies) == 0 {
				continue
			}
			ep := store.Episode{
				Season: e.season, Episodes: e.episodes, AirDate: e.airDate, Title: e.title,
				Folder: folder.Path, IDs: ids(e.name.IDs), ByNumber: e.byNumber, Copies: copies,
				Artwork: pics.of(stem(e.versions[0].parts[0].Name), domain.ArtworkThumb),
			}
			// An NFO's numbers are stated, not guessed, so they win over the file name's.
			if said := s.readNFO(ctx, lib, folder.Path, nfoOf(e.versions[0].parts)); said != nil {
				ep.NFO = &said.Metadata
				if len(said.Episodes) > 0 {
					ep.Episodes, ep.ByNumber = said.Episodes, true
				}
				if said.Season != nil {
					ep.Season = *said.Season
				}
			}
			episodes = append(episodes, ep)
		}
	}
	plans, inExtrasFolder := extrasIn(folder)
	extras, err := s.extras(ctx, lib, folder, plans, report, func(e extraPlan) store.Owner {
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
		return store.Saved{}, err
	}
	return s.store.SaveShowFolder(ctx, lib.ID, folder.Path, folder.Fingerprint[:], show, episodes, extras)
}

var errNoEpisode = errors.New("its name says no season or episode")

// copies reads each copy's content key and probes only copies the catalogue does not hold. A copy
// that cannot be read is left out and logged. A byte-identical copy in a second place is a known
// copy: it becomes another place to read the same version.
func (s *Scanner) copies(ctx context.Context, lib domain.Library, dir string, plans []copyPlan, report *Report) ([]store.Copy, error) {
	var copies []store.Copy
	for _, v := range plans {
		c, ok, err := s.copy(ctx, lib, dir, v, report)
		if err != nil {
			return nil, err
		}
		if ok {
			copies = append(copies, c)
		}
	}
	return copies, nil
}

func (s *Scanner) copy(ctx context.Context, lib domain.Library, dir string, v copyPlan, report *Report) (store.Copy, bool, error) {
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
	key, err := library.ContentKey(lib.Root, paths)
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
		facts, err := s.probe(ctx, lib.Root, paths[i])
		report.Probed++
		if err != nil {
			s.skip(ctx, report, paths[i], err)
			return c, false, nil
		}
		c.Parts[i].Facts = &facts
	}
	return c, true, nil
}

func (s *Scanner) probe(ctx context.Context, root, rel string) (media.Facts, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return media.Facts{}, err
	}
	defer f.Close()
	return s.prober.Probe(ctx, f)
}

// readNFO reads the first of the named NFOs in dir that exists, where the library takes NFOs. One that cannot be read is
// logged and the title goes on without it.
func (s *Scanner) readNFO(ctx context.Context, lib domain.Library, dir string, names ...string) *nfo.File {
	if !slices.Contains(lib.Sources, domain.SourceNFO) {
		return nil
	}
	for _, name := range names {
		rel := path.Join(dir, name)
		f, err := library.Open(lib.Root, rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			var said nfo.File
			said, err = nfo.Read(f)
			f.Close()
			if err == nil {
				return &said
			}
		}
		s.log.WarnContext(ctx, "NFO not read", slog.String("file", rel), slog.Any("err", err))
		return nil
	}
	return nil
}

func fileNames(f library.Folder) []string {
	names := make([]string, len(f.Files))
	for i, file := range f.Files {
		names[i] = file.Name
	}
	return names
}

func metadata(f *nfo.File) *domain.Metadata {
	if f == nil {
		return nil
	}
	return &f.Metadata
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
