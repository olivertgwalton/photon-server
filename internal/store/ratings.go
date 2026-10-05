package store

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// SaveRatings replaces what a source says sites make of a title.
func (s *Store) SaveRatings(ctx context.Context, id uuid.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	return s.q.Transaction(func(tx *query.Query) error {
		return saveRatings(ctx, tx, model.UUID(id), source, ratings)
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

// ratings answers a title's rating from each site, each from the source its library ranks
// highest, in the order of domain.RatingSites.
func (s *Store) ratings(ctx context.Context, item model.UUID) ([]domain.Rating, error) {
	r := s.q.Rating
	rows, err := r.WithContext(ctx).Where(r.ItemID.Eq(item)).Find()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ranked, err := ranks(ctx, s.q, item)
	if err != nil {
		return nil, err
	}
	best := map[domain.RatingSite]*model.Rating{}
	for _, row := range rows {
		if b, ok := best[row.Site]; !ok || ranked[row.Source] > ranked[b.Source] {
			best[row.Site] = row
		}
	}
	out := make([]domain.Rating, 0, len(best))
	for _, row := range best {
		out = append(out, domain.Rating{Site: row.Site, Score: float64(row.Score), Votes: deref(row.Votes)})
	}
	order := domain.RatingSites()
	slices.SortFunc(out, func(a, b domain.Rating) int {
		return cmp.Compare(slices.Index(order, a.Site), slices.Index(order, b.Site))
	})
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
