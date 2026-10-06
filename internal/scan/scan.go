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
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/nfo"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type prober interface {
	Probe(ctx context.Context, f *os.File) (domain.Facts, error)
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

// readsAtOnce is how many files a scan reads at once, and how many of the library's top-level
// folders it is in at once. Reading a file is a few requests in turn, each waiting on the disk or,
// on a network or debrid mount, on the network; four at once keep such a mount busy without asking
// more of it than it answers at once.
const readsAtOnce = 4

// run is one scan of a library, shared by the folders it reads at once.
type run struct {
	lib          domain.Library
	fingerprints map[string][]byte
	reads        chan struct{}
	progress     func(domain.ScanProgress)
	changed      func(store.Changed)
	// saving writes one folder at a time: two folders holding the same bytes, or the same show,
	// would otherwise each add them.
	saving sync.Mutex
	mu     sync.Mutex
	report Report
	told   domain.ScanProgress
	// folders and present are every folder read and every video and subtitle file in them.
	folders, present []string
}

// read takes one of the run's slots for reading files, to hand back with the function it answers.
func (r *run) read(ctx context.Context) (func(), error) {
	select {
	case r.reads <- struct{}{}:
		return func() { <-r.reads }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *run) count(f func(*Report)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&r.report)
}

// done tells that one more folder has been read.
func (r *run) done(folder string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.told.Done++
	r.told.Folder = strings.TrimPrefix(folder, ".")
	r.progress(r.told)
}

// wholePast is how many folders asked for at once a scan reads the whole library instead: so many
// changed together is an import, which reaches most of it anyway.
const wholePast = 100

// scopes are the folders a scan of those asked for reads: the whole library where it was asked
// for or past wholePast, else each folder not under another.
func scopes(asked []string) []string {
	if len(asked) == 0 || len(asked) > wholePast || slices.Contains(asked, ".") {
		return []string{"."}
	}
	var out []string
	for _, f := range slices.Sorted(slices.Values(asked)) {
		if len(out) == 0 || (f != out[len(out)-1] && !strings.HasPrefix(f, out[len(out)-1]+"/")) {
			out = append(out, f)
		}
	}
	return out
}

// Scan reads folders of a library again with everything under them, "." being the whole
// library, telling progress after each, and what each changed of the library's titles. Its
// top-level folders, or the folders asked for, are walked and read several at once, the folders
// under each one after another, parents first, so a show's seasons follow the show.
func (s *Scanner) Scan(ctx context.Context, lib domain.Library, asked []string, progress func(domain.ScanProgress), changed func(store.Changed)) (Report, error) {
	scoped := scopes(asked)
	r := &run{
		lib: lib, reads: make(chan struct{}, readsAtOnce), progress: progress, changed: changed,
		// The folders asked for are known before they are read; each folder read makes its
		// subfolders known.
		told: domain.ScanProgress{Library: lib.ID, Phase: domain.ScanReading, Known: len(scoped)},
	}
	var err error
	if r.fingerprints, err = s.store.FolderFingerprints(ctx, lib.ID); err != nil {
		return r.report, err
	}
	dirs := scoped
	if slices.Equal(scoped, []string{"."}) {
		var root library.Folder
		for folder, err := range library.Walk(lib.Root, ".") {
			// A root that cannot be read is a mount that is down, not a library emptied.
			if err != nil {
				return r.report, err
			}
			root = folder
			break
		}
		r.told.Known += len(root.Folders)
		if err := s.folder(ctx, r, root, nil); err != nil {
			return r.report, err
		}
		dirs = root.Folders
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(readsAtOnce)
	for _, dir := range dirs {
		g.Go(func() error {
			for folder, err := range library.Walk(lib.Root, dir) {
				if err != nil && folder.Path == "." {
					return err
				}
				r.mu.Lock()
				r.told.Known += len(folder.Folders)
				r.mu.Unlock()
				if err := s.folder(gctx, r, folder, err); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return r.report, err
	}
	if err := ctx.Err(); err != nil {
		return r.report, err
	}
	// A folder an empty .ignore hides was known and never read.
	r.told.Phase, r.told.Known, r.told.Folder = domain.ScanRemoving, r.told.Done, ""
	progress(r.told)
	titles, err := s.store.FinishScan(ctx, lib.ID, scoped, r.folders, r.present)
	if err == nil {
		changed(titles)
	}
	return r.report, err
}

// folder reads one folder the walk found, or could not read.
func (s *Scanner) folder(ctx context.Context, r *run, folder library.Folder, err error) error {
	defer r.done(folder.Path)
	if err != nil {
		s.log.WarnContext(ctx, "folder not read", slog.String("folder", folder.Path), slog.Any("err", err))
		return nil
	}
	r.mu.Lock()
	r.report.Folders++
	r.folders = append(r.folders, folder.Path)
	for _, f := range folder.Files {
		if naming.IsVideo(f.Name) || naming.IsSubtitle(f.Name) {
			r.present = append(r.present, path.Join(folder.Path, f.Name))
		}
	}
	r.mu.Unlock()
	for _, sk := range folder.Skipped {
		s.skip(ctx, r, path.Join(folder.Path, sk.Name), sk.Err)
	}
	if bytes.Equal(r.fingerprints[folder.Path], folder.Fingerprint[:]) {
		r.count(func(rep *Report) { rep.Unchanged++ })
		return nil
	}
	rd := reading{run: r, dir: folder.Path}
	if rd.known, err = s.store.KnownFiles(ctx, r.lib.ID, videosIn(folder)); err != nil {
		return err
	}
	var saved store.Saved
	switch r.lib.Kind {
	case domain.LibraryMovies:
		saved, err = s.saveFilms(ctx, rd, folder)
	case domain.LibraryShows:
		saved, err = s.saveEpisodes(ctx, rd, folder)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", folder.Path, err)
	}
	s.unowned(ctx, r, saved.Unowned)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changed(saved.Titles)
	return nil
}

// reading is one changed folder of a library being read, and the files the last scan recorded in
// it.
type reading struct {
	run   *run
	dir   string
	known map[string]store.KnownFile
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
	lib := r.run.lib
	var films []store.Film
	plans := planFilms(folder)
	pics := picturesIn(lib.Root, folder.Path, fileNames(folder))
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
		// A folder's own pictures and tunes are its film's when it holds one, as Jellyfin reads
		// them.
		var themes []string
		if len(plans) == 1 && folder.Path != "." {
			art = append(art, pics.own...)
			themes = themesIn(folder)
		}
		films = append(films, store.Film{
			Title: f.name.Title, Year: f.name.Year, Folder: folder.Path, IDs: ids(f.name.IDs),
			NFO: metadata(s.readNFO(ctx, lib, folder.Path, f.nfos...)), Artwork: art, Themes: themes, Copies: copies,
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
	r.run.saving.Lock()
	defer r.run.saving.Unlock()
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

func (s *Scanner) unowned(ctx context.Context, r *run, paths []string) {
	for _, p := range paths {
		s.skip(ctx, r, p, errNoOwner)
	}
}

func (s *Scanner) saveEpisodes(ctx context.Context, r reading, folder library.Folder) (store.Saved, error) {
	lib := r.run.lib
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
	pics := picturesIn(lib.Root, folder.Path, fileNames(folder))
	switch {
	case series != "" && folder.Path == series:
		show.Artwork = append([]domain.Artwork{}, pics.own...)
		show.SeasonArtwork = pics.seasons
		show.Themes = themesIn(folder)
	case series != "" && season != nil:
		show.SeasonArtwork = map[int][]domain.Artwork{
			*season: append(pics.own, seasonPictures(lib.Root, series, *season)...),
		}
	}
	var episodes []store.Episode
	if holdsEpisodes(folder.Path) {
		plans, unread := planEpisodes(folder, season, show.Title)
		for _, rel := range unread {
			s.skip(ctx, r.run, rel, errNoEpisode)
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
	r.run.saving.Lock()
	defer r.run.saving.Unlock()
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
// each title's copies that could be read. Files are read at once, as the run has room. A copy that
// cannot be read is left out and logged. A byte-identical copy in a second place is a known copy:
// it becomes another place to read the same version.
func (s *Scanner) readCopies(ctx context.Context, r reading, titles [][]copyPlan) ([][]store.Copy, error) {
	// fresh is a copy whose key was read now, so the catalogue is asked whether it holds it.
	type read struct {
		c         store.Copy
		ok, fresh bool
	}
	reads := make([][]read, len(titles))
	g, gctx := errgroup.WithContext(ctx)
	for i, plans := range titles {
		reads[i] = make([]read, len(plans))
		for j, v := range plans {
			c := describe(r.dir, v)
			if key, ok := r.unchanged(c.Parts); ok {
				c.ContentKey = key
				reads[i][j] = read{c: c, ok: true}
				continue
			}
			g.Go(func() error {
				done, err := r.run.read(gctx)
				if err != nil {
					return err
				}
				defer done()
				key, err := library.ContentKey(r.run.lib.Root, partPaths(c))
				if err != nil {
					s.skip(gctx, r.run, c.Parts[0].RelPath, err)
					return nil
				}
				c.ContentKey = key
				reads[i][j] = read{c: c, ok: true, fresh: true}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var keys [][]byte
	for i := range reads {
		for j := range reads[i] {
			if reads[i][j].fresh {
				keys = append(keys, reads[i][j].c.ContentKey)
			}
		}
	}
	known := map[string]bool{}
	if len(keys) > 0 {
		var err error
		if known, err = s.store.KnownCopies(ctx, r.run.lib.ID, keys); err != nil {
			return nil, err
		}
	}
	g, gctx = errgroup.WithContext(ctx)
	for i := range reads {
		for j := range reads[i] {
			rd := &reads[i][j]
			if !rd.fresh || known[string(rd.c.ContentKey)] {
				continue
			}
			g.Go(func() error {
				ok, err := s.probeParts(gctx, r, rd.c)
				rd.ok = ok
				return err
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([][]store.Copy, len(titles))
	for i := range reads {
		for _, rd := range reads[i] {
			if rd.ok {
				out[i] = append(out[i], rd.c)
			}
		}
	}
	return out, nil
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

// probeParts probes each part of a new copy, as the run has room, reporting whether every one
// could be.
func (s *Scanner) probeParts(ctx context.Context, r reading, c store.Copy) (bool, error) {
	for i, p := range c.Parts {
		done, err := r.run.read(ctx)
		if err != nil {
			return false, err
		}
		facts, err := s.probe(ctx, r.run.lib.Root, p.RelPath)
		done()
		r.run.count(func(rep *Report) { rep.Probed++ })
		if err != nil {
			s.skip(ctx, r.run, p.RelPath, err)
			return false, nil
		}
		c.Parts[i].Facts = &facts
	}
	return true, nil
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

func (s *Scanner) probe(ctx context.Context, root, rel string) (domain.Facts, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return domain.Facts{}, err
	}
	defer f.Close()
	return s.prober.Probe(ctx, f)
}

// readNFO reads the first of the named NFOs in dir that exists, where the library takes NFOs. One that cannot be read is
// logged and the title goes on without it.
func (s *Scanner) readNFO(ctx context.Context, lib domain.Library, dir string, names ...string) *nfo.File {
	if !lib.Takes(domain.SourceNFO) {
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

// themesIn answers a title's folder's theme tunes: theme.mp3 and its like, then its theme-music
// folder's, as the walk lists them.
func themesIn(f library.Folder) []string {
	var themes []string
	for _, file := range f.Files {
		if naming.Theme(file.Name) {
			themes = append(themes, path.Join(f.Path, file.Name))
		}
	}
	slices.SortStableFunc(themes, func(a, b string) int {
		return strings.Count(a, "/") - strings.Count(b, "/")
	})
	return themes
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

func (s *Scanner) skip(ctx context.Context, r *run, rel string, err error) {
	r.count(func(rep *Report) { rep.Skipped++ })
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
