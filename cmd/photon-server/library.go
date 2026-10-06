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
	"text/tabwriter"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const libraryUsage = `usage:
  photon-server library add -name NAME -kind movies|shows [SETTINGS] ROOT
  photon-server library set -name NAME SETTINGS
  photon-server library list
SETTINGS, any of:
  -sources nfo,tmdb,tvdb  -extras trailer,featurette|none  -monitor realtime|off
  -previews off|chapters|all  -markers off|chapters|all  -keyframes index|full|off`

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
	sources, extras, monitor, previews, markers, keyframes *string
}

func settingsFlags(fs *flag.FlagSet) librarySettings {
	return librarySettings{
		sources:   fs.String("sources", "", "where its metadata comes from, most trusted first"),
		extras:    fs.String("extras", "", "the kinds of video it keeps providers' links to, or none"),
		monitor:   fs.String("monitor", "", "realtime to scan it as its files change, off for the schedule alone"),
		previews:  fs.String("previews", "", "off, chapters for an image per chapter, or all for trickplay sheets too"),
		markers:   fs.String("markers", "", "off, chapters for the markers chapters name, or all to compare seasons' sound too"),
		keyframes: fs.String("keyframes", "", "index to read keyframes from a file's own index, full to read a file with none whole, or off"),
	}
}

// change answers what the flags ask for, and false where none was given.
func (f librarySettings) change() (store.LibraryChange, bool, error) {
	var change store.LibraryChange
	var err error
	if *f.sources != "" {
		if change.Sources, err = domain.ParseMetadataSources(*f.sources); err != nil {
			return change, false, err
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
	given := *f.sources != "" || *f.extras != "" || *f.monitor != "" || *f.previews != "" || *f.markers != "" || *f.keyframes != ""
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
	_, _ = fmt.Fprintln(w, "NAME\tKIND\tSOURCES\tEXTRAS\tMONITOR\tPREVIEWS\tMARKERS\tKEYFRAMES\tROOT\tID")
	for _, l := range libs {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%v\t%v\t%s\t%s\t%s\t%s\t%s\t%s\n", l.Name, l.Kind, l.Sources, l.RemoteExtras, l.Monitor, l.Previews, l.Markers, l.Keyframes, l.Root, l.ID)
	}
	return w.Flush()
}
