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
	"text/tabwriter"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const libraryUsage = `usage:
  photon-server library add -name NAME -kind movies|shows ROOT
  photon-server library set -name NAME -sources nfo,tmdb
  photon-server library list`

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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || fs.NArg() != 1 {
		return errors.New(libraryUsage)
	}
	k, err := domain.ParseLibraryKind(*kind)
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
	_, err = fmt.Fprintf(out, "added %s (%s) at %s\n", lib.Name, lib.Kind, lib.Root)
	return err
}

func setLibrary(ctx context.Context, st *store.Store, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("library set", flag.ContinueOnError)
	name := fs.String("name", "", "the library's name")
	sources := fs.String("sources", "", "where its metadata comes from, most trusted first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *sources == "" || fs.NArg() != 0 {
		return errors.New(libraryUsage)
	}
	list, err := domain.ParseMetadataSources(*sources)
	if err != nil {
		return err
	}
	lib, err := st.SetLibrarySources(ctx, *name, list)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s takes metadata from %v; its folders are read again at the next scan\n", lib.Name, lib.Sources)
	return err
}

func listLibraries(ctx context.Context, st *store.Store, out io.Writer) error {
	libs, err := st.Libraries(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tKIND\tSOURCES\tROOT\tID")
	for _, l := range libs {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%s\n", l.Name, l.Kind, l.Sources, l.Root, l.ID)
	}
	return w.Flush()
}
