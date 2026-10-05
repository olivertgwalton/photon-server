package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Show is a series as its folder names it.
type Show struct {
	Title  string
	Year   int
	Folder string
	IDs    map[domain.Provider]string
	NFO    *domain.Metadata
}

type Episode struct {
	Season   int
	Episodes []int
	AirDate  time.Time
	Title    string
	Folder   string
	IDs      map[domain.Provider]string
	NFO      *domain.Metadata
	// ByNumber lets the episode join one already known by its season and numbers. It is false for
	// an episode read from a bare number, which joins nothing but its own copies.
	ByNumber bool
	Copies   []Copy
}

// SaveShowFolder writes a folder of a series' episodes and extras and remembers its fingerprint,
// in one transaction. It answers the paths of extras no single title owns.
func (s *Store) SaveShowFolder(ctx context.Context, lib uuid.UUID, path string, fingerprint []byte, show Show, episodes []Episode, extras []Extra) ([]string, error) {
	var unowned []string
	err := s.q.Transaction(func(tx *query.Query) error {
		// A series' own folder, holding its NFO and extras, comes before any of its episodes.
		if len(episodes) > 0 || ((len(extras) > 0 || show.NFO != nil) && show.Folder != "") {
			showID, err := ensureShow(ctx, tx, lib, show)
			if err != nil {
				return err
			}
			for _, e := range episodes {
				if err := saveEpisode(ctx, tx, lib, showID, e); err != nil {
					return fmt.Errorf("%s season %d %v: %w", show.Title, e.Season, e.Episodes, err)
				}
			}
		}
		var err error
		if unowned, err = saveExtras(ctx, tx, lib, extras); err != nil {
			return err
		}
		return tx.Folder.WithContext(ctx).Save(&model.Folder{
			LibraryID: model.UUID(lib), Path: path, Fingerprint: fingerprint,
		})
	})
	return unowned, err
}

func ensureShow(ctx context.Context, tx *query.Query, lib uuid.UUID, show Show) (model.UUID, error) {
	i := tx.Item
	row := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemShow,
		ScanTitle: show.Title, Title: show.Title, SortTitle: sortTitle(show.Title), Folder: show.Folder,
	}
	known, err := i.WithContext(ctx).Where(
		i.LibraryID.Eq(model.UUID(lib)), i.Kind.Eq(string(domain.ItemShow)), i.Folder.Eq(show.Folder),
	).Take()
	switch {
	case err == nil:
		row.ID = known.ID
		_, err = i.WithContext(ctx).Where(i.ID.Eq(row.ID)).Select(i.ScanTitle).Updates(&row)
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err = i.WithContext(ctx).Create(&row); err == nil {
			err = enqueue(ctx, tx, domain.JobIdentify, row.ID)
		}
	}
	if err != nil {
		return model.UUID{}, err
	}
	return row.ID, describe(ctx, tx, row.ID, show.Title, show.Year, show.IDs, show.NFO)
}

func ensureSeason(ctx context.Context, tx *query.Query, lib uuid.UUID, showID model.UUID, folder string, number int) (model.UUID, error) {
	i := tx.Item
	known, err := i.WithContext(ctx).Where(
		i.ParentID.Eq(showID), i.Kind.Eq(string(domain.ItemSeason)), i.SeasonNumber.Eq(number),
	).Take()
	if err == nil {
		return known.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UUID{}, err
	}
	title := fmt.Sprintf("Season %d", number)
	if number == 0 {
		title = "Specials"
	}
	row := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemSeason, ParentID: &showID, SeasonNumber: &number,
		ScanTitle: title, Title: title, SortTitle: sortTitle(title), Folder: folder,
	}
	if err := i.WithContext(ctx).Create(&row); err != nil {
		return model.UUID{}, err
	}
	return row.ID, describe(ctx, tx, row.ID, title, 0, nil, nil)
}

func saveEpisode(ctx context.Context, tx *query.Query, lib uuid.UUID, showID model.UUID, e Episode) error {
	seasonID, err := ensureSeason(ctx, tx, lib, showID, e.Folder, e.Season)
	if err != nil {
		return err
	}
	row := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemEpisode, ParentID: &seasonID, SeasonNumber: &e.Season,
		ScanTitle: e.Title, Title: e.Title, SortTitle: sortTitle(e.Title), Folder: e.Folder,
	}
	if len(e.Episodes) > 0 {
		first, last := e.Episodes[0], e.Episodes[len(e.Episodes)-1]
		row.EpisodeNumber = &first
		if last != first {
			row.EpisodeEnd = &last
		}
	}
	if !e.AirDate.IsZero() {
		row.AirDate = &e.AirDate
	}
	row.ID, err = episodeItem(ctx, tx, lib, e, row)
	if err != nil {
		return err
	}
	i := tx.Item
	if row.ID == (model.UUID{}) {
		if err := i.WithContext(ctx).Create(&row); err != nil {
			return err
		}
		// The show's match describes its episodes, so a new one asks for it again.
		// ponytail: one added while that match runs is not described until the next is queued;
		// a metadata refresh task closes the gap.
		if err := enqueue(ctx, tx, domain.JobIdentify, showID); err != nil {
			return err
		}
	} else {
		_, err := i.WithContext(ctx).Where(i.ID.Eq(row.ID)).Select(
			i.ParentID, i.SeasonNumber, i.EpisodeNumber, i.EpisodeEnd, i.AirDate, i.ScanTitle, i.Folder,
		).Updates(&row)
		if err != nil {
			return err
		}
	}
	if err := describe(ctx, tx, row.ID, e.Title, 0, e.IDs, e.NFO); err != nil {
		return err
	}
	for _, c := range e.Copies {
		if err := saveCopy(ctx, tx, lib, row.ID, c); err != nil {
			return err
		}
	}
	return nil
}

// episodeItem is the episode a copy belongs to: the episode of a copy already known, else, for an
// episode read from a canonical form, the episode of its season with the same numbers or date.
func episodeItem(ctx context.Context, tx *query.Query, lib uuid.UUID, e Episode, row model.Item) (model.UUID, error) {
	if id, ok, err := knownItem(ctx, tx, lib, domain.ItemEpisode, e.Copies); err != nil || ok {
		return id, err
	}
	if !e.ByNumber {
		return model.UUID{}, nil
	}
	i := tx.Item
	q := i.WithContext(ctx).Where(i.ParentID.Eq(*row.ParentID), i.Kind.Eq(string(domain.ItemEpisode)))
	if row.EpisodeNumber != nil {
		q = q.Where(i.EpisodeNumber.Eq(*row.EpisodeNumber))
		if row.EpisodeEnd != nil {
			q = q.Where(i.EpisodeEnd.Eq(*row.EpisodeEnd))
		} else {
			q = q.Where(i.EpisodeEnd.IsNull())
		}
	} else {
		q = q.Where(i.EpisodeNumber.IsNull(), i.AirDate.Eq(*row.AirDate))
	}
	known, err := q.Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UUID{}, nil
	}
	if err != nil {
		return model.UUID{}, err
	}
	return known.ID, nil
}
