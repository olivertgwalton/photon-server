package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
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
func scanLibrary(st *store.Store, scanner *scan.Scanner, hub *events.Hub, logger *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		lib, err := st.Library(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		started := time.Now()
		r, err := scanner.Scan(ctx, lib, hub.Scanning(ctx))
		hub.Scanned(ctx, lib.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", lib.Name, err)
		}
		logger.InfoContext(ctx, "library scanned", slog.String("library", lib.Name),
			slog.Int("folders", r.Folders), slog.Int("unchanged", r.Unchanged),
			slog.Int("probed", r.Probed), slog.Int("left_out", r.Skipped))
		hub.Raise(ctx, domain.Event{Kind: domain.EventLibraryScanned, Library: lib.ID, Details: map[string]any{
			"folders": r.Folders, "unchanged": r.Unchanged, "probed": r.Probed, "left_out": r.Skipped,
		}})
		added, err := st.TitlesAddedSince(ctx, lib.ID, started)
		if err != nil {
			logger.WarnContext(ctx, "titles added not counted", slog.Any("err", err))
		}
		if added > 0 {
			hub.Raise(ctx, domain.Event{Kind: domain.EventTitlesAdded, Library: lib.ID, Details: map[string]any{"titles": added}})
		}
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
func backupTask(d backup.Dumper, hub *events.Hub, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskBackupDatabase,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: backupEvery}},
		Run: func(ctx context.Context) error {
			name, err := d.Dump(ctx, time.Now())
			if err == nil {
				logger.InfoContext(ctx, "database backed up", slog.String("file", name))
				hub.Raise(ctx, domain.Event{Kind: domain.EventBackupMade, Details: map[string]any{"file": name}})
			}
			return err
		},
	}
}

// activityKept is how long the activity log keeps an entry: Jellyfin's default.
const activityKept = 30 * 24 * time.Hour

// pruneActivityEvery is how often entries older than that are forgotten.
const pruneActivityEvery = 24 * time.Hour

func pruneActivityTask(st *store.Store, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskPruneActivity,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: pruneActivityEvery}},
		Run: func(ctx context.Context) error {
			n, err := st.PruneActivity(ctx, time.Now().Add(-activityKept))
			if n > 0 {
				logger.InfoContext(ctx, "old activity forgotten", slog.Int64("entries", n))
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

// sweepArtworkEvery is how often replaced pictures are cleared from the cache.
const sweepArtworkEvery = 7 * 24 * time.Hour

// sweepArtworkTask clears the cache of pictures no title or person has any more.
func sweepArtworkTask(st *store.Store, cache *artwork.Cache, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskSweepArtwork,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: sweepArtworkEvery}},
		Run: func(ctx context.Context) error {
			n, err := cache.Sweep(ctx, st.LivePictures)
			if n > 0 {
				logger.InfoContext(ctx, "replaced pictures cleared", slog.Int("files", n))
			}
			return err
		},
	}
}

// markersTask queues the comparison of every season with an episode whose sound has not been
// compared: those from before the server could, and those whose comparison was cut short.
func markersTask(st *store.Store, tools media.Tools, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskDetectMarkers,
		Triggers: []task.Trigger{{Kind: task.TriggerDaily, At: refreshAt}},
		Run: func(ctx context.Context) error {
			if !tools.Chromaprint {
				return nil
			}
			n, err := st.QueueMarkers(ctx)
			if n > 0 {
				logger.InfoContext(ctx, "seasons queued to have their intros and credits found", slog.Int64("seasons", n))
			}
			return err
		},
	}
}

// previewsAt is when previews are brought into line with their libraries: the small hours, before
// the metadata refresh.
const previewsAt = 2 * time.Hour

// previewsTask queues the parts whose previews are not what their library asks for, among them
// those of a library switched on since and those whose job died, and clears the folders of
// previews no part has any more.
func previewsTask(st *store.Store, previews *analysis.Previews, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskBackfillPreviews,
		Triggers: []task.Trigger{{Kind: task.TriggerDaily, At: previewsAt}},
		Run: func(ctx context.Context) error {
			n, err := st.QueuePreviews(ctx)
			if n > 0 {
				logger.InfoContext(ctx, "parts queued for previews", slog.Int64("parts", n))
			}
			if err != nil {
				return err
			}
			removed, err := previews.Sweep(ctx, st.LivePreviews)
			if removed > 0 {
				logger.InfoContext(ctx, "previews no part has cleared", slog.Int("folders", removed))
			}
			return err
		},
	}
}

// downloadsKept is how long a download stays once its file is ready, or its conversion failed,
// unless it is removed first: a week covers a device that is away or off for one before it
// fetches, and is as long as a converted file holds disk nobody may want.
const downloadsKept = 7 * 24 * time.Hour

// sweepDownloadsTask forgets the downloads kept past downloadsKept and the conversions no
// download needs; each node then prunes their files.
func sweepDownloadsTask(st *store.Store, logger *slog.Logger) task.Task {
	return task.Task{
		Key:      domain.TaskSweepDownloads,
		Triggers: []task.Trigger{{Kind: task.TriggerEvery, Every: time.Hour}},
		Run: func(ctx context.Context) error {
			n, err := st.ExpireDownloads(ctx, time.Now().Add(-downloadsKept))
			if n > 0 {
				logger.InfoContext(ctx, "downloads expired", slog.Int64("downloads", n))
			}
			return err
		},
	}
}
