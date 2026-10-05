package store

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

var ErrLibraryExists = errors.New("a library with that name or root already exists")

func (s *Store) AddLibrary(ctx context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error) {
	row := model.Library{Name: name, Kind: kind, Root: root}
	err := s.q.Transaction(func(tx *query.Query) error {
		if err := tx.Library.WithContext(ctx).Create(&row); err != nil {
			return err
		}
		if err := saveSources(ctx, tx, row.ID, domain.DefaultSources()); err != nil {
			return err
		}
		return saveRemoteExtras(ctx, tx, row.ID, domain.DefaultRemoteExtras())
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domain.Library{}, ErrLibraryExists
	}
	if err != nil {
		return domain.Library{}, fmt.Errorf("adding library: %w", err)
	}
	row.Monitor, row.RefreshDays, row.Previews, row.Markers = domain.MonitorRealtime, 30, domain.PreviewsAll, domain.MarkersAll
	return library(row, domain.DefaultSources(), domain.DefaultRemoteExtras()), nil
}

func (s *Store) Libraries(ctx context.Context) ([]domain.Library, error) {
	q, ls := s.q.Library, s.q.LibrarySource
	rows, err := q.WithContext(ctx).Order(q.Name).Find()
	if err != nil {
		return nil, err
	}
	taken, err := ls.WithContext(ctx).Order(ls.Position).Find()
	if err != nil {
		return nil, err
	}
	sources := map[model.UUID][]domain.FieldSource{}
	for _, t := range taken {
		sources[t.LibraryID] = append(sources[t.LibraryID], t.Source)
	}
	ex := s.q.LibraryRemoteExtra
	kept, err := ex.WithContext(ctx).Order(ex.Kind).Find()
	if err != nil {
		return nil, err
	}
	extras := map[model.UUID][]domain.ExtraKind{}
	for _, k := range kept {
		extras[k.LibraryID] = append(extras[k.LibraryID], k.Kind)
	}
	libs := make([]domain.Library, len(rows))
	for i, r := range rows {
		libs[i] = library(*r, sources[r.ID], extras[r.ID])
	}
	return libs, nil
}

// Library answers one library, or ErrNotFound.
func (s *Store) Library(ctx context.Context, id uuid.UUID) (domain.Library, error) {
	libs, err := s.Libraries(ctx)
	if err != nil {
		return domain.Library{}, err
	}
	for _, l := range libs {
		if l.ID == id {
			return l, nil
		}
	}
	return domain.Library{}, ErrNotFound
}

// LibraryChange is what to change about a library; an empty name, monitor, previews or markers,
// or a nil list, is left as it is.
type LibraryChange struct {
	Name         string
	Sources      []domain.FieldSource
	RemoteExtras []domain.ExtraKind
	Monitor      domain.Monitor
	// RefreshDays, where set, is how often its titles are matched again; zero never.
	RefreshDays *int
	// Previews is what pictures it makes of its videos; parts are brought into line by the
	// previews backfill.
	Previews domain.PreviewLevel
	// Markers is how it finds intros and credits; seasons not yet compared are queued by the daily
	// marker detection.
	Markers domain.MarkerDetection
}

// SetLibrary renames a library, changes whether it is watched, where its metadata comes from and
// in what order, and which kinds of video it keeps providers' links to. With new sources or kinds
// its titles are matched again, and with new sources its folders are read again at the next scan
// too, so the new order reaches everything already there.
func (s *Store) SetLibrary(ctx context.Context, id uuid.UUID, change LibraryChange) error {
	err := s.q.Transaction(func(tx *query.Query) error {
		l := tx.Library
		row, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(id))).Take()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if change.Name != "" {
			if _, err := l.WithContext(ctx).Where(l.ID.Eq(row.ID)).Update(l.Name, change.Name); err != nil {
				return err
			}
		}
		if change.Sources != nil {
			ls := tx.LibrarySource
			if _, err := ls.WithContext(ctx).Where(ls.LibraryID.Eq(row.ID)).Delete(); err != nil {
				return err
			}
			if err := saveSources(ctx, tx, row.ID, change.Sources); err != nil {
				return err
			}
			if _, err := tx.Folder.WithContext(ctx).Where(tx.Folder.LibraryID.Eq(row.ID)).Delete(); err != nil {
				return err
			}
		}
		if change.RefreshDays != nil {
			if _, err := l.WithContext(ctx).Where(l.ID.Eq(row.ID)).Update(l.RefreshDays, *change.RefreshDays); err != nil {
				return err
			}
		}
		if change.Monitor != "" {
			if _, err := l.WithContext(ctx).Where(l.ID.Eq(row.ID)).Update(l.Monitor, change.Monitor); err != nil {
				return err
			}
		}
		if change.Previews != "" {
			if _, err := l.WithContext(ctx).Where(l.ID.Eq(row.ID)).Update(l.Previews, change.Previews); err != nil {
				return err
			}
		}
		if change.Markers != "" {
			if _, err := l.WithContext(ctx).Where(l.ID.Eq(row.ID)).Update(l.Markers, change.Markers); err != nil {
				return err
			}
		}
		if change.Sources == nil && change.RemoteExtras == nil {
			return nil
		}
		if change.RemoteExtras != nil {
			ex := tx.LibraryRemoteExtra
			if _, err := ex.WithContext(ctx).Where(ex.LibraryID.Eq(row.ID)).Delete(); err != nil {
				return err
			}
			if err := saveRemoteExtras(ctx, tx, row.ID, change.RemoteExtras); err != nil {
				return err
			}
		}
		i := tx.Item
		titles, err := i.WithContext(ctx).Where(
			i.LibraryID.Eq(row.ID), i.Kind.In(string(domain.ItemMovie), string(domain.ItemShow)),
		).Find()
		if err != nil {
			return err
		}
		for _, t := range titles {
			if err := enqueue(ctx, tx, domain.JobIdentify, t.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrLibraryExists
	}
	return err
}

// RemoveLibrary forgets a library and everything in it; its files are left alone.
func (s *Store) RemoveLibrary(ctx context.Context, id uuid.UUID) error {
	l := s.q.Library
	res, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(id))).Delete()
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

func saveRemoteExtras(ctx context.Context, tx *query.Query, lib model.UUID, kinds []domain.ExtraKind) error {
	if len(kinds) == 0 {
		return nil
	}
	rows := make([]*model.LibraryRemoteExtra, len(kinds))
	for n, k := range kinds {
		rows[n] = &model.LibraryRemoteExtra{LibraryID: lib, Kind: k}
	}
	return tx.LibraryRemoteExtra.WithContext(ctx).Create(rows...)
}

func saveSources(ctx context.Context, tx *query.Query, lib model.UUID, sources []domain.FieldSource) error {
	if len(sources) == 0 {
		return nil
	}
	if err := registered(ctx, tx, sources); err != nil {
		return err
	}
	rows := make([]*model.LibrarySource, len(sources))
	for n, src := range sources {
		rows[n] = &model.LibrarySource{LibraryID: lib, Source: src, Position: n}
	}
	return tx.LibrarySource.WithContext(ctx).Create(rows...)
}

func library(r model.Library, sources []domain.FieldSource, extras []domain.ExtraKind) domain.Library {
	return domain.Library{
		ID: uuid.UUID(r.ID), Name: r.Name, Kind: r.Kind, Root: r.Root, Sources: sources, RemoteExtras: extras,
		Monitor: r.Monitor, RefreshDays: int(r.RefreshDays), Previews: r.Previews, Markers: r.Markers,
	}
}
