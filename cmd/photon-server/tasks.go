package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

// scanEvery is Jellyfin's default library scan interval.
const scanEvery = 12 * time.Hour

func scanTask(st *store.Store, scanner *scan.Scanner, logger *slog.Logger) task.Task {
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
				r, err := scanner.Scan(ctx, lib)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", lib.Name, err))
					continue
				}
				logger.InfoContext(ctx, "library scanned", slog.String("library", lib.Name),
					slog.Int("folders", r.Folders), slog.Int("unchanged", r.Unchanged),
					slog.Int("probed", r.Probed), slog.Int("left_out", r.Skipped))
			}
			return errors.Join(errs...)
		},
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
