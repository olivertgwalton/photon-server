package store

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SaveRatings replaces what a source says sites make of a title.
func (s *Store) SaveRatings(ctx context.Context, id uuid.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		asks, err := askedOf(ctx, tx, id, source)
		if err != nil {
			return err
		}
		return saveRatings(ctx, tx, id, source, asks.of(asks.title, domain.Metadata{Ratings: ratings}).Ratings)
	})
}

func saveRatings(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	if _, err := tx.Exec(ctx, `DELETE FROM ratings WHERE item_id = $1 AND source = $2`, item, source); err != nil {
		return err
	}
	if len(ratings) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, x := range ratings {
		var votes *int
		if x.Votes > 0 {
			votes = &x.Votes
		}
		b.Queue(`INSERT INTO ratings (item_id, source, site, score, votes) VALUES ($1, $2, $3, $4, $5)`,
			item, source, x.Site, float32(x.Score), votes)
	}
	return tx.SendBatch(ctx, b).Close()
}

// ratings answers each title's rating from each site, each from the source its library ranks
// highest, in the order of domain.RatingSites.
func (s *Store) ratings(ctx context.Context, items []*model.Item) (map[uuid.UUID][]domain.Rating, error) {
	out := map[uuid.UUID][]domain.Rating{}
	if len(items) == 0 {
		return out, nil
	}
	rows, err := queryRows[model.Rating](ctx, s.pool,
		`SELECT item_id, source, site, score, votes FROM ratings WHERE item_id = ANY($1)`, ids(items))
	if err != nil || len(rows) == 0 {
		return out, err
	}
	taken, err := rankings(ctx, s.pool, items, domain.FetcherMetadata)
	if err != nil {
		return nil, err
	}
	ranked := map[uuid.UUID]map[domain.FieldSource]int{}
	for _, it := range items {
		ranked[it.ID] = rankOf(taken[it.ID])
	}
	best := map[uuid.UUID]map[domain.RatingSite]*model.Rating{}
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
	return providerSettings(ctx, s.pool, id)
}

func providerSettings(ctx context.Context, q db, id domain.FieldSource) (map[string]string, error) {
	out := map[string]string{}
	var raw []byte
	err := q.QueryRow(ctx, `SELECT settings FROM providers WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out)
}

// SetProviderSettings changes a provider's settings: a value set replaces what was there, and an
// empty one clears it.
func (s *Store) SetProviderSettings(ctx context.Context, id domain.FieldSource, change map[string]string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		set, err := providerSettings(ctx, tx, id)
		if err != nil {
			return err
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
		_, err = tx.Exec(ctx, `
			INSERT INTO providers (id, settings) VALUES ($1, $2)
			ON CONFLICT (id) DO UPDATE SET settings = excluded.settings`, id, raw)
		return err
	})
}
