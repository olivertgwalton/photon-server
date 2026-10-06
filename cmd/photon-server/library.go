package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const libraryUsage = `usage:
  photon-server library add -name NAME -kind movies|shows [SETTINGS] ROOT
  photon-server library set -name NAME SETTINGS
  photon-server library list
SETTINGS, any of:
  -metadata 'show=nfo,tvdb,tmdb;episode=nfo,tmdb'  -images 'show=tvdb,tmdb'
  -extras trailer,featurette|none  -monitor realtime|off
  -previews off|chapters|all  -markers off|chapters|all  -keyframes index|full|off
  -themes all|local|off`

func library(ctx context.Context, logger *slog.Logger, databaseURL string, out io.Writer, args []string) error {
	if len(args) == 0 {
		return errors.New(libraryUsage)
	}
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "add":
		return addLibrary(ctx, st, out, args[1:])
	case "set":
		return setLibrary(ctx, st, out, args[1:])
	case "list":
		return listLibraries(ctx, st, out)
	}
	return errors.New(libraryUsage)
}

func addLibrary(ctx context.Context, st *store.Store, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("library add", flag.ContinueOnError)
	name := fs.String("name", "", "the library's name")
	kind := fs.String("kind", "", "movies or shows")
	settings := settingsFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	change, given, err := settings.change()
	if err != nil {
		return err
	}
	if *name == "" || fs.NArg() != 1 {
		return errors.New(libraryUsage)
	}
	k, err := domain.Parse("library kind", *kind, domain.LibraryKinds())
	if err != nil {
		return err
	}
	if err := domain.CheckSources(k, change.Sources); err != nil {
		return err
	}
	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if info, err := os.Stat(root); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", root)
	}
	lib, err := st.AddLibrary(ctx, *name, k, root)
	if err != nil {
		return err
	}
	// Set before the first scan is queued, so it reads the library as asked.
	if given {
		if err := st.SetLibrary(ctx, lib.ID, change); err != nil {
			return err
		}
	}
	if err := st.ScanLibrary(ctx, lib.ID, 0); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "added %s (%s) at %s; its scan is queued\n", lib.Name, lib.Kind, lib.Root)
	return err
}

// librarySettings are the flags that set how a library is read, and the change they ask for.
type librarySettings struct {
	metadata, images, extras, monitor, previews, markers, keyframes, themes *string
}

func settingsFlags(fs *flag.FlagSet) librarySettings {
	return librarySettings{
		metadata:  fs.String("metadata", "", "where each kind of item's metadata comes from, most trusted first, as kind=source,source;kind=…"),
		images:    fs.String("images", "", "where each kind of item's pictures come from, most trusted first, as kind=source,source;kind=…"),
		extras:    fs.String("extras", "", "the kinds of video it keeps providers' links to, or none"),
		monitor:   fs.String("monitor", "", "realtime to scan it as its files change, off for the schedule alone"),
		previews:  fs.String("previews", "", "off, chapters for an image per chapter, or all for trickplay sheets too"),
		markers:   fs.String("markers", "", "off, chapters for the markers chapters name, or all to compare seasons' sound too"),
		keyframes: fs.String("keyframes", "", "index to read keyframes from a file's own index, full to read a file with none whole, or off"),
		themes:    fs.String("themes", "", "all for theme tunes beside titles and, for a show with none, Plex's theme host; local for the files alone; or off"),
	}
}

// change answers what the flags ask for, and false where none was given.
func (f librarySettings) change() (store.LibraryChange, bool, error) {
	var change store.LibraryChange
	var err error
	for fetcher, list := range map[domain.Fetcher]string{domain.FetcherMetadata: *f.metadata, domain.FetcherImages: *f.images} {
		if list != "" {
			if change.Sources, err = parseSources(change.Sources, fetcher, list); err != nil {
				return change, false, err
			}
		}
	}
	if *f.extras != "" {
		if change.RemoteExtras, err = domain.ParseExtraKinds(*f.extras); err != nil {
			return change, false, err
		}
	}
	if *f.monitor != "" {
		if change.Monitor, err = domain.Parse("monitor", *f.monitor, domain.Monitors()); err != nil {
			return change, false, err
		}
	}
	if *f.previews != "" {
		if change.Previews, err = domain.Parse("previews", *f.previews, domain.PreviewLevels()); err != nil {
			return change, false, err
		}
	}
	if *f.markers != "" {
		if change.Markers, err = domain.Parse("markers", *f.markers, domain.MarkerDetections()); err != nil {
			return change, false, err
		}
	}
	if *f.keyframes != "" {
		if change.Keyframes, err = domain.Parse("keyframes", *f.keyframes, domain.KeyframeModes()); err != nil {
			return change, false, err
		}
	}
	if *f.themes != "" {
		if change.Themes, err = domain.Parse("themes", *f.themes, domain.ThemeLookups()); err != nil {
			return change, false, err
		}
	}
	given := *f.metadata != "" || *f.images != "" || *f.extras != "" || *f.monitor != "" || *f.previews != "" || *f.markers != "" || *f.keyframes != "" || *f.themes != ""
	return change, given, nil
}

func setLibrary(ctx context.Context, st *store.Store, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("library set", flag.ContinueOnError)
	name := fs.String("name", "", "the library's name")
	settings := settingsFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	change, given, err := settings.change()
	if err != nil {
		return err
	}
	if *name == "" || !given || fs.NArg() != 0 {
		return errors.New(libraryUsage)
	}
	libs, err := st.Libraries(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(libs, func(l domain.Library) bool { return l.Name == *name })
	if i < 0 {
		return fmt.Errorf("no library is called %q", *name)
	}
	if err := domain.CheckSources(libs[i].Kind, change.Sources); err != nil {
		return err
	}
	if err := st.SetLibrary(ctx, libs[i].ID, change); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s changed\n", *name)
	return err
}

func listLibraries(ctx context.Context, st *store.Store, out io.Writer) error {
	libs, err := st.Libraries(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tKIND\tMETADATA\tIMAGES\tEXTRAS\tMONITOR\tPREVIEWS\tMARKERS\tKEYFRAMES\tTHEMES\tROOT\tID")
	for _, l := range libs {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", l.Name, l.Kind,
			formatSources(l.Sources, domain.FetcherMetadata), formatSources(l.Sources, domain.FetcherImages),
			l.RemoteExtras, l.Monitor, l.Previews, l.Markers, l.Keyframes, l.Themes, l.Root, l.ID)
	}
	return w.Flush()
}

// parseSources adds to sources the rankings for f a flag gives, as kind=source,source;kind=…, each
// source listed enabled; a kind with none listed asks nothing.
func parseSources(sources []domain.KindSources, f domain.Fetcher, list string) ([]domain.KindSources, error) {
	for given := range strings.SplitSeq(list, ";") {
		kind, names, ok := strings.Cut(strings.TrimSpace(given), "=")
		if !ok {
			return nil, fmt.Errorf("%q is not kind=source,source", given)
		}
		k, err := domain.Parse("item kind", kind, domain.ItemKinds())
		if err != nil {
			return nil, err
		}
		ranked := []domain.RankedSource{}
		for name := range strings.SplitSeq(names, ",") {
			if name = strings.TrimSpace(name); name != "" {
				ranked = append(ranked, domain.RankedSource{Source: domain.FieldSource(name), Enabled: true})
			}
		}
		i := slices.IndexFunc(sources, func(s domain.KindSources) bool { return s.Kind == k })
		if i < 0 {
			sources, i = append(sources, domain.KindSources{Kind: k}), len(sources)
		}
		if f == domain.FetcherMetadata {
			sources[i].Metadata = ranked
		} else {
			sources[i].Images = ranked
		}
	}
	return sources, nil
}

// formatSources writes the sources a library asks for f, as the flags take them.
func formatSources(sources []domain.KindSources, f domain.Fetcher) string {
	var kinds []string
	for _, k := range sources {
		var names []string
		for _, r := range k.Of(f) {
			if r.Enabled {
				names = append(names, string(r.Source))
			}
		}
		kinds = append(kinds, string(k.Kind)+"="+strings.Join(names, ","))
	}
	return strings.Join(kinds, ";")
}
