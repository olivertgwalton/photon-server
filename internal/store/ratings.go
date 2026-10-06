package store

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// SaveRatings replaces what a source says sites make of a title.
func (s *Store) SaveRatings(ctx context.Context, id uuid.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	return s.q.Transaction(func(tx *query.Query) error {
		asks, err := askedOf(ctx, tx, model.UUID(id), source)
		if err != nil {
			return err
		}
		return saveRatings(ctx, tx, model.UUID(id), source, asks.of(asks.title, domain.Metadata{Ratings: ratings}).Ratings)
	})
}

func saveRatings(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	r := tx.Rating
	if _, err := r.WithContext(ctx).Where(r.ItemID.Eq(item), r.Source.Eq(string(source))).Delete(); err != nil {
		return err
	}
	rows := make([]*model.Rating, 0, len(ratings))
	for _, x := range ratings {
		row := &model.Rating{ItemID: item, Source: source, Site: x.Site, Score: float32(x.Score)}
		if x.Votes > 0 {
			row.Votes = &x.Votes
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return r.WithContext(ctx).Create(rows...)
}

// ratings answers each title's rating from each site, each from the source its library ranks
// highest, in the order of domain.RatingSites.
func (s *Store) ratings(ctx context.Context, items []*model.Item) (map[model.UUID][]domain.Rating, error) {
	out := map[model.UUID][]domain.Rating{}
	if len(items) == 0 {
		return out, nil
	}
	r := s.q.Rating
	rows, err := r.WithContext(ctx).Where(r.ItemID.In(ids(items)...)).Find()
	if err != nil || len(rows) == 0 {
		return out, err
	}
	taken, err := rankings(ctx, s.q, items, domain.FetcherMetadata)
	if err != nil {
		return nil, err
	}
	ranked := map[model.UUID]map[domain.FieldSource]int{}
	for _, it := range items {
		ranked[it.ID] = rankOf(taken[it.ID])
	}
	best := map[model.UUID]map[domain.RatingSite]*model.Rating{}
	for _, row := range rows {
		rank := ranked[row.ItemID]
		if best[row.ItemID] == nil {
			best[row.ItemID] = map[domain.RatingSite]*model.Rating{}
		}
		if b, ok := best[row.ItemID][row.Site]; !ok || rank[row.Source] > rank[b.Source] {
			best[row.ItemID][row.Site] = row
		}
	}
	order := domain.RatingSites()
	for item, sites := range best {
		list := make([]domain.Rating, 0, len(sites))
		for _, row := range sites {
			// Kept as a real; a tenth of a point is as fine as any site scores.
			list = append(list, domain.Rating{Site: row.Site, Score: math.Round(float64(row.Score)*10) / 10, Votes: deref(row.Votes)})
		}
		slices.SortFunc(list, func(a, b domain.Rating) int {
			return cmp.Compare(slices.Index(order, a.Site), slices.Index(order, b.Site))
		})
		out[item] = list
	}
	return out, nil
}

// ProviderSettings answers what an admin has set for a provider.
func (s *Store) ProviderSettings(ctx context.Context, id domain.FieldSource) (map[string]string, error) {
	p := s.q.Provider
	rows, err := p.WithContext(ctx).Where(p.ID.Eq(string(id))).Find()
	if err != nil || len(rows) == 0 {
		return map[string]string{}, err
	}
	out := map[string]string{}
	return out, json.Unmarshal(rows[0].Settings, &out)
}

// SetProviderSettings changes a provider's settings: a value set replaces what was there, and an
// empty one clears it.
func (s *Store) SetProviderSettings(ctx context.Context, id domain.FieldSource, change map[string]string) error {
	return s.q.Transaction(func(tx *query.Query) error {
		p := tx.Provider
		rows, err := p.WithContext(ctx).Where(p.ID.Eq(string(id))).Find()
		if err != nil {
			return err
		}
		set := map[string]string{}
		if len(rows) > 0 {
			if err := json.Unmarshal(rows[0].Settings, &set); err != nil {
				return err
			}
		}
		for k, v := range change {
			if v == "" {
				delete(set, k)
			} else {
				set[k] = v
			}
		}
		raw, err := json.Marshal(set)
		if err != nil {
			return err
		}
		return p.WithContext(ctx).Save(&model.Provider{ID: string(id), Settings: raw})
	})
}
