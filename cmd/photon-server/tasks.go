package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

// scanEvery is Jellyfin's default library scan interval.
const scanEvery = 12 * time.Hour

// scanTask queues every library's scan; a library being watched is usually scanned already.
func scanTask(st *store.Store) task.Task {
	return task.Task{
		Key:      domain.TaskScanLibraries,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: scanEvery}},
		Run: func(ctx context.Context) error {
			libs, err := st.Libraries(ctx)
			if err != nil {
				return err
			}
			var errs []error
			for _, lib := range libs {
				errs = append(errs, st.ScanLibrary(ctx, lib.ID, 0))
			}
			return errors.Join(errs...)
		},
	}
}

// scanLibrary is the job that scans one library; jobs are one per library, so two scans of a
// library never run at once.
func scanLibrary(st *store.Store, scanner *scan.Scanner, logger *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		lib, err := st.Library(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		r, err := scanner.Scan(ctx, lib)
		if err != nil {
			return fmt.Errorf("%s: %w", lib.Name, err)
		}
		logger.InfoContext(ctx, "library scanned", slog.String("library", lib.Name),
			slog.Int("folders", r.Folders), slog.Int("unchanged", r.Unchanged),
			slog.Int("probed", r.Probed), slog.Int("left_out", r.Skipped))
		return nil
	}
}

func sweepTask(st *store.Store, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskSweepJobs,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: time.Minute}},
		Run: func(ctx context.Context) error {
			n, err := st.SweepJobs(ctx)
			if n > 0 {
				logger.InfoContext(ctx, "jobs requeued after their worker's lease ran out", slog.Int64("jobs", n))
			}
			return err
		},
	}
}

// backupEvery is how often Plex backs its database up.
const backupEvery = 3 * 24 * time.Hour

// backupTask dumps the database, keeping the newest few.
func backupTask(d backup.Dumper, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskBackupDatabase,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: backupEvery}},
		Run: func(ctx context.Context) error {
			name, err := d.Dump(ctx, time.Now())
			if err == nil {
				logger.InfoContext(ctx, "database backed up", slog.String("file", name))
			}
			return err
		},
	}
}

// refreshAt is when the titles due a fresh match are queued: the small hours, as Plex's scheduled
// maintenance runs.
const refreshAt = 3 * time.Hour

// refreshTask queues a match of every title its library says is due one.
func refreshTask(st *store.Store, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskRefreshMetadata,
		Triggers: []task.Trigger{{Kind: task.TriggerDaily, At: refreshAt}},
		Run: func(ctx context.Context) error {
			n, err := st.RefreshStale(ctx)
			if n > 0 {
				logger.InfoContext(ctx, "titles queued to be matched again", slog.Int64("titles", n))
			}
			return err
		},
	}
}
