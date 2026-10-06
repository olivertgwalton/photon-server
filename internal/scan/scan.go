package scan

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
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
	fingerprints, err := s.store.FolderFingerprints(ctx, lib.ID)
	if err != nil {
		return report, err
	}
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
		if bytes.Equal(fingerprints[folder.Path], folder.Fingerprint[:]) {
			report.Unchanged++
			progress(told)
			continue
		}
		r := reading{lib: lib, dir: folder.Path, report: &report}
		if r.known, err = s.store.KnownFiles(ctx, lib.ID, videosIn(folder)); err != nil {
			return report, err
		}
		var saved store.Saved
		switch lib.Kind {
		case domain.LibraryMovies:
			saved, err = s.saveFilms(ctx, r, folder)
		case domain.LibraryShows:
			saved, err = s.saveEpisodes(ctx, r, folder)
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

// reading is one changed folder of a library being read: the files the last scan recorded in it,
// and the scan's report.
type reading struct {
	lib    domain.Library
	dir    string
	known  map[string]store.KnownFile
	report *Report
}

func videosIn(f library.Folder) []string {
	var paths []string
	for _, file := range f.Files {
		if naming.IsVideo(file.Name) {
			paths = append(paths, path.Join(f.Path, file.Name))
		}
	}
	return paths
}

func (s *Scanner) saveFilms(ctx context.Context, r reading, folder library.Folder) (store.Saved, error) {
	lib := r.lib
	var films []store.Film
	plans := planFilms(folder)
	pics := picturesIn(folder.Path, fileNames(folder))
	read, err := s.readCopies(ctx, r, plansOf(plans, func(f film) []copyPlan { return f.versions }))
	if err != nil {
		return store.Saved{}, err
	}
	for i, f := range plans {
		copies := read[i]
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
	extras, err := s.extras(ctx, r, folder, extraPlans, func(e extraPlan) store.Owner {
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
func (s *Scanner) extras(ctx context.Context, r reading, folder library.Folder, plans []extraPlan, owner func(extraPlan) store.Owner) ([]store.Extra, error) {
	read, err := s.readCopies(ctx, r, plansOf(plans, func(e extraPlan) []copyPlan { return []copyPlan{e.copy} }))
	if err != nil {
		return nil, err
	}
	var extras []store.Extra
	for i, e := range plans {
		for _, c := range read[i] {
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

func (s *Scanner) saveEpisodes(ctx context.Context, r reading, folder library.Folder) (store.Saved, error) {
	lib, report := r.lib, r.report
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
		read, err := s.readCopies(ctx, r, plansOf(plans, func(e episodePlan) []copyPlan { return e.versions }))
		if err != nil {
			return store.Saved{}, err
		}
		for i, e := range plans {
			copies := read[i]
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
	extras, err := s.extras(ctx, r, folder, plans, func(e extraPlan) store.Owner {
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

func plansOf[T any](titles []T, copies func(T) []copyPlan) [][]copyPlan {
	out := make([][]copyPlan, len(titles))
	for i, t := range titles {
		out[i] = copies(t)
	}
	return out
}

// readCopies reads the content key of every copy of each title of a folder that changed since the
// last scan, asks the catalogue once which it holds, and probes only those it does not, answering
// each title's copies that could be read. A copy that cannot be read is left out and logged. A
// byte-identical copy in a second place is a known copy: it becomes another place to read the
// same version.
func (s *Scanner) readCopies(ctx context.Context, r reading, titles [][]copyPlan) ([][]store.Copy, error) {
	read := make([][]store.Copy, len(titles))
	var keys [][]byte
	known := map[string]bool{}
	for i, plans := range titles {
		for _, v := range plans {
			c := describe(r.dir, v)
			if key, ok := r.unchanged(c.Parts); ok {
				c.ContentKey = key
				known[string(key)] = true
				read[i] = append(read[i], c)
				continue
			}
			key, err := library.ContentKey(r.lib.Root, partPaths(c))
			if err != nil {
				s.skip(ctx, r.report, c.Parts[0].RelPath, err)
				continue
			}
			c.ContentKey = key
			read[i] = append(read[i], c)
			keys = append(keys, key)
		}
	}
	if len(keys) > 0 {
		held, err := s.store.KnownCopies(ctx, r.lib.ID, keys)
		if err != nil {
			return nil, err
		}
		maps.Copy(known, held)
	}
	for i := range read {
		read[i] = slices.DeleteFunc(read[i], func(c store.Copy) bool {
			return !known[string(c.ContentKey)] && !s.probeParts(ctx, r, c)
		})
	}
	return read, nil
}

// describe is a planned copy's parts and subtitles, its bytes not yet read.
func describe(dir string, v copyPlan) store.Copy {
	c := store.Copy{Edition: v.edition, Label: v.label}
	for _, sub := range v.subtitles {
		c.Subtitles = append(c.Subtitles, store.Subtitle{
			RelPath: path.Join(dir, sub.file.Name), Size: sub.file.Size, ModTime: sub.file.ModTime,
			Codec: sub.codec, Language: sub.tags.Language, Title: sub.tags.Title,
			Forced: sub.tags.Forced, Default: sub.tags.Default, HearingImpaired: sub.tags.HearingImpaired,
		})
	}
	for _, p := range v.parts {
		c.Parts = append(c.Parts, store.Part{RelPath: path.Join(dir, p.Name), Size: p.Size, ModTime: p.ModTime})
	}
	return c
}

func partPaths(c store.Copy) []string {
	paths := make([]string, len(c.Parts))
	for i, p := range c.Parts {
		paths[i] = p.RelPath
	}
	return paths
}

// probeParts probes each part of a new copy, reporting whether every one could be.
func (s *Scanner) probeParts(ctx context.Context, r reading, c store.Copy) bool {
	for i, p := range c.Parts {
		facts, err := s.probe(ctx, r.lib.Root, p.RelPath)
		r.report.Probed++
		if err != nil {
			s.skip(ctx, r.report, p.RelPath, err)
			return false
		}
		c.Parts[i].Facts = &facts
	}
	return true
}

// unchanged answers the content key of a copy whose every part is where the last scan found it,
// at the size and modification time it had then, as Plex and Silo skip a file: its bytes are not
// read again.
func (r reading) unchanged(parts []store.Part) ([]byte, bool) {
	var key []byte
	for i, p := range parts {
		f, ok := r.known[p.RelPath]
		if !ok || f.Size != p.Size || !f.ModTime.Equal(p.ModTime) || f.Idx != i || f.Parts != len(parts) ||
			(key != nil && !bytes.Equal(f.ContentKey, key)) {
			return nil, false
		}
		key = f.ContentKey
	}
	return key, true
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
