package store

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var ErrLibraryExists = errors.New("a library with that name or root already exists")

func (s *Store) AddLibrary(ctx context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error) {
	row := model.Library{Name: name, Kind: kind, Root: root}
	if err := s.q.Library.WithContext(ctx).Create(&row); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.Library{}, ErrLibraryExists
		}
		return domain.Library{}, fmt.Errorf("adding library: %w", err)
	}
	return library(row), nil
}

func (s *Store) Libraries(ctx context.Context) ([]domain.Library, error) {
	q := s.q.Library
	rows, err := q.WithContext(ctx).Order(q.Name).Find()
	if err != nil {
		return nil, err
	}
	libs := make([]domain.Library, len(rows))
	for i, r := range rows {
		libs[i] = library(*r)
	}
	return libs, nil
}

func library(r model.Library) domain.Library {
	return domain.Library{ID: uuid.UUID(r.ID), Name: r.Name, Kind: r.Kind, Root: r.Root}
}
