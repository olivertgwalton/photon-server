package store

import (
	"context"
	"errors"
	"uuid"

	"gorm.io/gen"
	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Owner names the title an extra belongs to.
type Owner struct {
	Kind domain.ItemKind
	// Folder is a film's own folder, or the series folder of a show, season or episode.
	Folder string
	// Title tells apart films that share a folder.
	Title   string
	Season  int
	Episode int
}

type Extra struct {
	Kind   domain.ExtraKind
	Title  string
	Folder string
	Owner  Owner
	Copy   Copy
}

var errNoOwner = errors.New("no single title owns it")

// saveExtras saves each extra under its owner, answering the paths of those whose owner the
// catalogue does not hold, or holds twice, so the scanner can say so rather than guess.
func saveExtras(ctx context.Context, tx *query.Query, lib uuid.UUID, extras []Extra) ([]string, error) {
	var unowned []string
	for _, e := range extras {
		owner, err := ownerOf(ctx, tx, lib, e.Owner)
		if errors.Is(err, errNoOwner) {
			unowned = append(unowned, e.Copy.Parts[0].RelPath)
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := saveExtra(ctx, tx, lib, owner, e); err != nil {
			return nil, err
		}
	}
	return unowned, nil
}

func ownerOf(ctx context.Context, tx *query.Query, lib uuid.UUID, o Owner) (model.UUID, error) {
	i := tx.Item
	inLibrary := i.LibraryID.Eq(model.UUID(lib))
	var conds []gen.Condition
	switch o.Kind {
	case domain.ItemMovie:
		conds = []gen.Condition{inLibrary, i.Kind.Eq(string(domain.ItemMovie)), i.Folder.Eq(o.Folder)}
		if o.Title != "" {
			conds = append(conds, i.Title.Eq(o.Title))
		}
	case domain.ItemShow, domain.ItemSeason, domain.ItemEpisode:
		show, err := one(i.WithContext(ctx).Where(inLibrary, i.Kind.Eq(string(domain.ItemShow)), i.Folder.Eq(o.Folder)))
		if err != nil || o.Kind == domain.ItemShow {
			return show, err
		}
		season, err := one(i.WithContext(ctx).Where(i.ParentID.Eq(show), i.Kind.Eq(string(domain.ItemSeason)), i.SeasonNumber.Eq(o.Season)))
		if err != nil || o.Kind == domain.ItemSeason {
			return season, err
		}
		conds = []gen.Condition{i.ParentID.Eq(season), i.Kind.Eq(string(domain.ItemEpisode)), i.EpisodeNumber.Eq(o.Episode)}
	case domain.ItemExtra:
		return model.UUID{}, errNoOwner
	}
	return one(i.WithContext(ctx).Where(conds...))
}

// one is the single item a query finds; none or several is no owner.
func one(q query.IItemDo) (model.UUID, error) {
	rows, err := q.Limit(2).Find()
	if err != nil {
		return model.UUID{}, err
	}
	if len(rows) != 1 {
		return model.UUID{}, errNoOwner
	}
	return rows[0].ID, nil
}

func saveExtra(ctx context.Context, tx *query.Query, lib uuid.UUID, owner model.UUID, e Extra) error {
	row := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemExtra, ParentID: &owner, ExtraKind: &e.Kind,
		Title: e.Title, SortTitle: sortTitle(e.Title), Folder: e.Folder,
	}
	v, i := tx.Version, tx.Item
	version, err := v.WithContext(ctx).Where(v.LibraryID.Eq(model.UUID(lib)), v.Fingerprint.Eq(e.Copy.ContentKey)).Take()
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	known := err == nil
	var id model.UUID
	if known {
		item, err := i.WithContext(ctx).Where(i.ID.Eq(version.ItemID)).Take()
		if err != nil {
			return err
		}
		// The same bytes are already a film's or an episode's copy: this file is another place to
		// read it, and that title stays what it is.
		if item.Kind != domain.ItemExtra {
			return addPlaces(ctx, tx, lib, version.ID, e.Copy)
		}
		id = item.ID
	}
	if known {
		row.ID = id
		_, err = i.WithContext(ctx).Where(i.ID.Eq(id)).Select(i.ParentID, i.ExtraKind, i.Title, i.SortTitle, i.Folder).Updates(&row)
	} else {
		err = i.WithContext(ctx).Create(&row)
	}
	if err != nil {
		return err
	}
	return saveCopy(ctx, tx, lib, row.ID, e.Copy)
}
