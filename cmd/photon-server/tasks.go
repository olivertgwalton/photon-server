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
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

// scanEvery is Jellyfin's default library scan interval.
const scanEvery = 12 * time.Hour

// scanTask queues every library's scan; a library being watched is usually scanned already.
func scanTask(st *store.Store) task.Task {
	return task.Task{
		Key:     domain.TaskScanLibraries,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: scanEvery},
		Run: func(ctx context.Context, _ task.Start) error {
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

// scanLibrary is the job that scans the folders of one library asked for; jobs are one per
// library, so two scans of a library never run at once.
func scanLibrary(st *store.Store, scanner *scan.Scanner, hub *events.Hub, logger *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		lib, err := st.Library(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		asked, read, err := st.ScanRequests(ctx, id)
		if err != nil {
			return err
		}
		started := time.Now()
		r, err := scanner.Scan(ctx, lib, asked, hub.Scanning(ctx), func(c store.Changed) { hub.Changed(ctx, lib.ID, c) })
		hub.Scanned(ctx, lib.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", lib.Name, err)
		}
		if err := st.ScanAnswered(ctx, id, asked, read); err != nil {
			return err
		}
		if err := st.RefreshSmartCollections(ctx, lib.ID); err != nil {
			logger.WarnContext(ctx, "smart collections not refreshed", slog.String("library", lib.Name), slog.Any("err", err))
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
		Key:     domain.TaskSweepJobs,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: time.Minute},
		Run: func(ctx context.Context, _ task.Start) error {
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
		Key:     domain.TaskBackupDatabase,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: backupEvery},
		Run: func(ctx context.Context, _ task.Start) error {
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
		Key:     domain.TaskPruneActivity,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: pruneActivityEvery},
		Run: func(ctx context.Context, _ task.Start) error {
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
		Key:     domain.TaskRefreshMetadata,
		Trigger: task.Trigger{Kind: task.TriggerDaily, At: refreshAt},
		Run: func(ctx context.Context, _ task.Start) error {
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

// sweepArtworkTask clears the cache of pictures no title or person has any more, takes the
// BlurHash of every picture kept without one, and fetches the pictures titles show first.
func sweepArtworkTask(st *store.Store, cache *artwork.Cache, logger *slog.Logger) task.Task {
	return task.Task{
		Key:     domain.TaskSweepArtwork,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: sweepArtworkEvery},
		Run: func(ctx context.Context, _ task.Start) error {
			n, err := cache.Sweep(ctx, st.LivePictures)
			if n > 0 {
				logger.InfoContext(ctx, "replaced pictures cleared", slog.Int("files", n))
			}
			if err != nil {
				return err
			}
			n, err = backfillBlurhashes(ctx, st, cache)
			if n > 0 {
				logger.InfoContext(ctx, "pictures given a blurhash", slog.Int("pictures", n))
			}
			if err != nil {
				return err
			}
			n, err = backfillPictures(ctx, st, cache)
			if n > 0 {
				logger.InfoContext(ctx, "pictures fetched ahead", slog.Int("pictures", n))
			}
			return err
		},
	}
}

// picturesBatch is how many titles' pictures are fetched at once.
const picturesBatch = 100

// backfillPictures fetches the pictures every title shows first that are not yet fetched: those
// of titles described before the server fetched them as it did, and those it could not then.
func backfillPictures(ctx context.Context, st *store.Store, cache *artwork.Cache) (int, error) {
	fetched := 0
	var after uuid.UUID
	for {
		pictures, last, err := st.Unfetched(ctx, after, picturesBatch)
		if err != nil {
			return fetched, err
		}
		n, err := cache.Fetch(ctx, pictures)
		fetched += n
		if err != nil || last == (uuid.UUID{}) {
			return fetched, err
		}
		after = last
	}
}

// blurhashBatch is how many pictures without a BlurHash are asked for at once.
const blurhashBatch = 500

// backfillBlurhashes takes the BlurHash of each library file and each cached picture that has
// none: those kept before the server took them. A provider's picture not fetched yet is left to
// be hashed as it is, and one not decoded here, such as SVG, is passed over.
func backfillBlurhashes(ctx context.Context, st *store.Store, cache *artwork.Cache) (int, error) {
	hashed := 0
	var after uuid.UUID
	for {
		batch, err := st.Unhashed(ctx, after, blurhashBatch)
		if err != nil || len(batch) == 0 {
			return hashed, err
		}
		for _, p := range batch {
			var hash string
			if p.Path == "" {
				hash, err = cache.Blurhash(ctx, p.ID)
			} else {
				hash, err = artwork.FileBlurhash(p.Root, p.Path)
			}
			if err != nil {
				continue
			}
			if err := st.SetBlurhash(ctx, p.ID, hash); err != nil {
				return hashed, err
			}
			hashed++
		}
		after = batch[len(batch)-1].ID
	}
}

// markersTask queues the comparison of every season with an episode whose sound has not been
// compared, where the server can compare sound, and the reading of every film's end not read:
// those from before the server could, and those cut short. It runs as the maintenance window
// opens, window, and what it queues then is due in the window; what an admin asks for is due now.
func markersTask(st *store.Store, tools media.Tools, window task.Trigger, logger *slog.Logger) task.Task {
	return task.Task{
		Key:     domain.TaskDetectMarkers,
		Trigger: window,
		Run: func(ctx context.Context, start task.Start) error {
			films, err := st.QueueFilmMarkers(ctx, due(start))
			if films > 0 {
				logger.InfoContext(ctx, "films queued to have their credits found", slog.Int64("films", films))
			}
			if err != nil || !tools.Chromaprint {
				return err
			}
			seasons, err := st.QueueSeasonMarkers(ctx, due(start))
			if seasons > 0 {
				logger.InfoContext(ctx, "seasons queued to have their intros and credits found", slog.Int64("seasons", seasons))
			}
			return err
		},
	}
}

// missingPreviewsKept is how long a part on no disk keeps its previews. Jellyfin and Plex drop a
// missing file's at the next scan or emptied trash; a month covers a share down for repair or
// over a holiday, which would otherwise come back to hours of remaking, for a few megabytes a
// title.
const missingPreviewsKept = 30 * 24 * time.Hour

// previewsTask queues the parts whose previews are not what their library asks for, among them
// those of a library switched on since and those whose job died, forgets the previews of parts
// missing past missingPreviewsKept, and clears the folders of previews no part has any more. It
// runs as the maintenance window opens, window, and what it queues then is due in the window; what
// an admin asks for is due now.
func previewsTask(st *store.Store, previews *analysis.Previews, window task.Trigger, logger *slog.Logger) task.Task {
	return task.Task{
		Key:     domain.TaskBackfillPreviews,
		Trigger: window,
		Run: func(ctx context.Context, start task.Start) error {
			n, err := st.QueuePreviews(ctx, due(start))
			if n > 0 {
				logger.InfoContext(ctx, "parts queued for previews", slog.Int64("parts", n))
			}
			if err != nil {
				return err
			}
			n, err = st.ForgetMissingPreviews(ctx, time.Now().Add(-missingPreviewsKept))
			if n > 0 {
				logger.InfoContext(ctx, "previews of missing parts forgotten", slog.Int64("parts", n))
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
		Key:     domain.TaskSweepDownloads,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: time.Hour},
		Run: func(ctx context.Context, _ task.Start) error {
			n, err := st.ExpireDownloads(ctx, time.Now().Add(-downloadsKept))
			if n > 0 {
				logger.InfoContext(ctx, "downloads expired", slog.Int64("downloads", n))
			}
			return err
		},
	}
}

// due is when the work a run queues is due: in the window for a run its trigger began, as
// Jellyfin's trigger carries a run's time limit, and now for one an admin asked for, as Jellyfin's
// manual run carries none.
func due(start task.Start) domain.JobDue {
	switch start {
	case task.StartTrigger:
		return domain.JobDueWindow
	case task.StartRequest:
	}
	return domain.JobDueNow
}

// refreshCollectionsEvery is how stale a smart collection may be of what is described after a
// scan: matched, refreshed or edited. A scan refreshes its library's at once.
const refreshCollectionsEvery = 15 * time.Minute

// refreshCollectionsTask finds every smart collection's titles again.
func refreshCollectionsTask(st *store.Store) task.Task {
	return task.Task{
		Key:     domain.TaskRefreshCollections,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: refreshCollectionsEvery},
		Run:     func(ctx context.Context, _ task.Start) error { return st.RefreshSmartCollections(ctx) },
	}
}

// syncListsEvery is how often list collections read their lists again: Kometa's daily run.
const syncListsEvery = 24 * time.Hour

// syncListsTask keeps every list collection's titles what its list holds. A list that cannot be
// read leaves its collection as it was.
func syncListsTask(st *store.Store, providers *provider.Registry) task.Task {
	return task.Task{
		Key:     domain.TaskSyncLists,
		Trigger: task.Trigger{Kind: task.TriggerEvery, Every: syncListsEvery},
		Run: func(ctx context.Context, _ task.Start) error {
			lists, err := st.ListCollections(ctx)
			if err != nil {
				return err
			}
			var errs []error
			for _, c := range lists {
				listed, err := providers.List(ctx, c.List.Source, c.List.ID)
				if err == nil {
					err = st.SetListMembers(ctx, c.ID, listed)
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("list %s %s: %w", c.List.Source, c.List.ID, err))
				}
			}
			return errors.Join(errs...)
		},
	}
}
