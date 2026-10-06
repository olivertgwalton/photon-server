package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// scanLibraries scans every library, or only those named.
func scanLibraries(ctx context.Context, logger *slog.Logger, databaseURL string, out io.Writer, names []string) error {
	tools, err := media.FindTools(ctx)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	libs, err := st.Libraries(ctx)
	if err != nil {
		return err
	}
	scanner := scan.New(st, tools, logger)
	matched := 0
	for _, lib := range libs {
		if len(names) > 0 && !slices.Contains(names, lib.Name) {
			continue
		}
		matched++
		r, err := scanner.Scan(ctx, lib, func(domain.ScanProgress) {}, func(store.Changed) {})
		if err != nil {
			return fmt.Errorf("%s: %w", lib.Name, err)
		}
		_, err = fmt.Fprintf(out, "%s: %d folders (%d unchanged), %d files probed, %d left out\n",
			lib.Name, r.Folders, r.Unchanged, r.Probed, r.Skipped)
		if err != nil {
			return err
		}
	}
	if matched < len(names) {
		return fmt.Errorf("no library is called one of %q", names)
	}
	return nil
}
