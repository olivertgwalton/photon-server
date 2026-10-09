package scan

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/nfo"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type prober interface {
	Probe(ctx context.Context, in media.Input) (domain.Facts, error)
}

// lister reads the list a remote library holds the titles of.
type lister interface {
	List(ctx context.Context, source domain.FieldSource, id string) ([]domain.Listed, error)
}

type Scanner struct {
	store  *store.Store
	prober prober
	lists  lister
	log    *slog.Logger
}

func New(st *store.Store, p prober, lists lister, log *slog.Logger) *Scanner {
	return &Scanner{store: st, prober: p, lists: lists, log: log}
}

// Report counts what a scan did; Probed is zero for a library nothing in has changed.
type Report struct {
	Folders   int
	Unchanged int
	Probed    int
	Skipped   int
}

// readsAtOnce is how many files a scan reads at once. Reading a file is a few requests in turn,
// each waiting on the disk or, on a network or debrid mount, on the network. On riven over
// Real-Debrid, probing new films took 243 ms each four at once, 182 ms eight at once and no less
// sixteen at once; and a read the mount never answers holds its slot until the probe times out.
const readsAtOnce = 8

// foldersAtOnce is how many folders a scan is in at once: listing one, or waiting on its files'
// reads or its save. Riven answers a mount's requests on four threads, and NFS and SMB more.
const foldersAtOnce = 8

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

// Scan reads a library again: a folder library's folders asked for, with everything under them,
// "." being the whole library; a remote library's list. It tells progress as it goes, and what it
// changed of the library's titles.
func (s *Scanner) Scan(ctx context.Context, lib domain.Library, asked []string, progress func(domain.ScanProgress), changed func(store.Changed)) (Report, error) {
	switch lib.Media {
	case domain.MediaRemote:
		return s.list(ctx, lib, progress, changed)
	case domain.MediaFolder:
	}
	return s.walk(ctx, lib, asked, progress, changed)
}

// list makes a remote library hold the titles its list holds now. A list that cannot be read
// fails the scan rather than emptying the library, as a root that cannot be read does.
func (s *Scanner) list(ctx context.Context, lib domain.Library, progress func(domain.ScanProgress), changed func(store.Changed)) (Report, error) {
	told := domain.ScanProgress{Library: lib.ID, Phase: domain.ScanReading, Known: 1}
	progress(told)
	// A library of what a search finds alone keeps only what a profile played, favourited or
	// watchlisted of it.
	var listed []domain.Listed
	var err error
	if lib.ListSource != "" {
		if listed, err = s.lists.List(ctx, lib.ListSource, lib.ListID); err != nil {
			return Report{}, err
		}
	}
	titles, err := s.store.SaveListed(ctx, lib.ID, lib.Kind.ItemKinds()[0], listed)
	if err != nil {
		return Report{}, err
	}
	changed(titles)
	told.Done = 1
	progress(told)
	return Report{Folders: 1}, nil
}

// walk reads folders of a folder library again with everything under them. Folders are read
// several at once, parents first, so a show's seasons follow the show.
func (s *Scanner) walk(ctx context.Context, lib domain.Library, asked []string, progress func(domain.ScanProgress), changed func(store.Changed)) (Report, error) {
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
	err = library.Walk(ctx, lib.Root, scoped, foldersAtOnce, func(ctx context.Context, folder library.Folder, err error) error {
		// A root that cannot be read is a mount that is down, not a library emptied.
		if err != nil && folder.Path == "." {
			return err
		}
		r.mu.Lock()
		r.told.Known += len(folder.Folders)
		r.mu.Unlock()
		return s.folder(ctx, r, folder, err)
	})
	if err != nil {
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

var errNoOwner = errors.New("no single title it could belong to")

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
	rd := reading{run: r, dir: folder.Path, unread: new(atomic.Bool)}
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
	for _, p := range saved.Unowned {
		s.skip(ctx, r, p, errNoOwner)
	}
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
	// unread is set by a file that could not be read, a mount's passing failure as often as not.
	unread *atomic.Bool
}

// fingerprint is the folder's to remember, or nil to have the next scan read it again for a file
// that could not be read now, as Plex and Jellyfin try a file again at their next scan.
func (r reading) fingerprint(f library.Folder) []byte {
	if r.unread.Load() {
		return nil
	}
	return f.Fingerprint[:]
}

func (s *Scanner) unreadable(ctx context.Context, r reading, rel string, err error) {
	r.unread.Store(true)
	s.skip(ctx, r.run, rel, err)
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
	return s.store.SaveFolder(ctx, lib.ID, folder.Path, r.fingerprint(folder), films, extras)
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
	return s.store.SaveShowFolder(ctx, lib.ID, folder.Path, r.fingerprint(folder), show, episodes, extras)
}

var errNoEpisode = errors.New("its name says no season or episode")

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
