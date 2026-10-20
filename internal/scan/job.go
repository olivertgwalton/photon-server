package scan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// hub tells clients how a scan is going, what it changed, and what came of it.
type hub interface {
	Scanning(ctx context.Context) func(domain.ScanProgress)
	Changed(ctx context.Context, lib uuid.UUID, titles store.Changed)
	Scanned(ctx context.Context, lib uuid.UUID)
	Raise(ctx context.Context, e domain.Event)
}

// Job is the job that scans the folders of one library asked for; jobs are one per library, so
// two scans of a library never run at once.
func (s *Scanner) Job(hub hub) jobs.Handler {
	st, logger := s.store, s.log
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
		r, err := s.Scan(ctx, lib, asked, hub.Scanning(ctx), func(c store.Changed) { hub.Changed(ctx, lib.ID, c) })
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
		hub.Raise(ctx, domain.Event{Kind: domain.EventLibraryScanned, Library: lib.ID, Details: domain.ScannedDetails{
			Folders: r.Folders, Unchanged: r.Unchanged, Probed: r.Probed, LeftOut: r.Skipped,
		}})
		added, err := st.TitlesAddedSince(ctx, lib.ID, started)
		if err != nil {
			logger.WarnContext(ctx, "titles added not counted", slog.Any("err", err))
		}
		if added > 0 {
			hub.Raise(ctx, domain.Event{Kind: domain.EventTitlesAdded, Library: lib.ID, Details: domain.TitlesAddedDetails{Titles: added}})
			told, err := st.Notify(ctx, lib.ID, started)
			if err != nil {
				logger.WarnContext(ctx, "profiles not told of titles added", slog.Any("err", err))
			}
			for _, t := range told {
				hub.Raise(ctx, domain.Event{Kind: domain.EventNotificationsChanged, Profile: t.ProfileID, Details: domain.NotificationsDetails{
					Change: domain.NotificationsAdded, IDs: t.IDs,
				}})
			}
		}
		// New titles have their subtitles fetched now, as Jellyfin fetches a new item's, not at the
		// next daily run.
		if added > 0 && len(lib.SubtitleLanguages) > 0 {
			if err := st.RequestTask(ctx, domain.TaskFetchSubtitles); err != nil {
				logger.WarnContext(ctx, "missing subtitles not asked for", slog.Any("err", err))
			}
		}
		return nil
	}
}
