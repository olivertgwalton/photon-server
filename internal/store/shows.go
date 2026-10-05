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
}

type Episode struct {
	Season   int
	Episodes []int
	AirDate  time.Time
	Title    string
	Folder   string
	IDs      map[domain.Provider]string
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
		// A series' extras folder can come before any of its episodes, and its extras need the show.
		if len(episodes) > 0 || (len(extras) > 0 && show.Folder != "") {
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
		Title: show.Title, SortTitle: sortTitle(show.Title), Folder: show.Folder,
	}
	if show.Year != 0 {
		row.Year = &show.Year
	}
	known, err := i.WithContext(ctx).Where(
		i.LibraryID.Eq(model.UUID(lib)), i.Kind.Eq(string(domain.ItemShow)), i.Folder.Eq(show.Folder),
	).Take()
	switch {
	case err == nil:
		row.ID = known.ID
		_, err = i.WithContext(ctx).Where(i.ID.Eq(row.ID)).Select(i.Title, i.SortTitle, i.Year).Updates(&row)
	case errors.Is(err, gorm.ErrRecordNotFound):
		err = i.WithContext(ctx).Create(&row)
	}
	if err != nil {
		return model.UUID{}, err
	}
	return row.ID, saveIDs(ctx, tx, row.ID, show.IDs)
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
		Title: title, SortTitle: fmt.Sprintf("%06d", number), Folder: folder,
	}
	err = i.WithContext(ctx).Create(&row)
	return row.ID, err
}

func saveEpisode(ctx context.Context, tx *query.Query, lib uuid.UUID, showID model.UUID, e Episode) error {
	seasonID, err := ensureSeason(ctx, tx, lib, showID, e.Folder, e.Season)
	if err != nil {
		return err
	}
	row := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemEpisode, ParentID: &seasonID, SeasonNumber: &e.Season,
		Title: e.Title, SortTitle: sortTitle(e.Title), Folder: e.Folder,
	}
	if len(e.Episodes) > 0 {
		first, last := e.Episodes[0], e.Episodes[len(e.Episodes)-1]
		row.EpisodeNumber = &first
		row.SortTitle = fmt.Sprintf("%06d", first)
		if last != first {
			row.EpisodeEnd = &last
		}
	}
	if !e.AirDate.IsZero() {
		row.AirDate = &e.AirDate
		if row.EpisodeNumber == nil {
			row.SortTitle = e.AirDate.Format(time.DateOnly)
		}
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
	} else {
		_, err := i.WithContext(ctx).Where(i.ID.Eq(row.ID)).Select(
			i.ParentID, i.SeasonNumber, i.EpisodeNumber, i.EpisodeEnd, i.AirDate, i.Title, i.SortTitle, i.Folder,
		).Updates(&row)
		if err != nil {
			return err
		}
	}
	if err := saveIDs(ctx, tx, row.ID, e.IDs); err != nil {
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
