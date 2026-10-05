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
		return saveSources(ctx, tx, row.ID, domain.DefaultSources())
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domain.Library{}, ErrLibraryExists
	}
	if err != nil {
		return domain.Library{}, fmt.Errorf("adding library: %w", err)
	}
	return library(row, domain.DefaultSources()), nil
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
	libs := make([]domain.Library, len(rows))
	for i, r := range rows {
		libs[i] = library(*r, sources[r.ID])
	}
	return libs, nil
}

// SetLibrarySources changes where a library's metadata comes from and in what order. Its folders
// are read again at the next scan and its titles matched again, so the new order applies to
// everything already there.
func (s *Store) SetLibrarySources(ctx context.Context, name string, sources []domain.FieldSource) (domain.Library, error) {
	var lib domain.Library
	err := s.q.Transaction(func(tx *query.Query) error {
		l := tx.Library
		row, err := l.WithContext(ctx).Where(l.Name.Eq(name)).Take()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		ls := tx.LibrarySource
		if _, err := ls.WithContext(ctx).Where(ls.LibraryID.Eq(row.ID)).Delete(); err != nil {
			return err
		}
		if err := saveSources(ctx, tx, row.ID, sources); err != nil {
			return err
		}
		if _, err := tx.Folder.WithContext(ctx).Where(tx.Folder.LibraryID.Eq(row.ID)).Delete(); err != nil {
			return err
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
		lib = library(*row, sources)
		return nil
	})
	return lib, err
}

func saveSources(ctx context.Context, tx *query.Query, lib model.UUID, sources []domain.FieldSource) error {
	if len(sources) == 0 {
		return nil
	}
	rows := make([]*model.LibrarySource, len(sources))
	for n, src := range sources {
		rows[n] = &model.LibrarySource{LibraryID: lib, Source: src, Position: n}
	}
	return tx.LibrarySource.WithContext(ctx).Create(rows...)
}

func library(r model.Library, sources []domain.FieldSource) domain.Library {
	return domain.Library{ID: uuid.UUID(r.ID), Name: r.Name, Kind: r.Kind, Root: r.Root, Sources: sources}
}
