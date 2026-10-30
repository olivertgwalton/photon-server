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

// SaveRatings replaces what a source says sites make of a title, and the age it says Common Sense
// Media advises it for.
func (s *Store) SaveRatings(ctx context.Context, id uuid.UUID, source domain.FieldSource, rated domain.Rated) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		asks, err := askedOf(ctx, tx, id, source)
		if err != nil {
			return err
		}
		if err := saveRatings(ctx, tx, id, source, asks.of(asks.title, domain.Metadata{Ratings: rated.Ratings}).Ratings); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM common_sense_ages WHERE item_id = $1 AND source = $2`, id, source); err != nil || rated.CommonSenseAge == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO common_sense_ages (item_id, source, age) VALUES ($1, $2, $3)`, id, source, rated.CommonSenseAge)
		return err
	})
}

// commonSenseAge answers the age Common Sense Media advises a title for, 0 for none. Should two
// sources differ, the older stands.
func (s *Store) commonSenseAge(ctx context.Context, id uuid.UUID) (int, error) {
	var age *int
	err := s.pool.QueryRow(ctx, `SELECT max(age) FROM common_sense_ages WHERE item_id = $1`, id).Scan(&age)
	return deref(age), err
}

func saveRatings(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, ratings []domain.Rating) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM ratings WHERE item_id = $1 AND source = $2`, item, source)
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

// ratings answers each title's rating from each site, as ranked_ratings ranks them, in the order
// of domain.RatingSites.
func (s *Store) ratings(ctx context.Context, items []*model.Item) (map[uuid.UUID][]domain.Rating, error) {
	out := map[uuid.UUID][]domain.Rating{}
	if len(items) == 0 {
		return out, nil
	}
	rows, err := queryRows[model.Rating](ctx, s.pool, `
		SELECT r.item_id, r.source, r.site, r.score, r.votes FROM items i, ranked_ratings(i) r WHERE i.id = ANY($1)`, ids(items))
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		// Kept as a real; a tenth of a point is as fine as any site scores.
		out[row.ItemID] = append(out[row.ItemID], domain.Rating{Site: row.Site, Score: math.Round(float64(row.Score)*10) / 10, Votes: deref(row.Votes)})
	}
	order := domain.RatingSites()
	for _, list := range out {
		slices.SortFunc(list, func(a, b domain.Rating) int {
			return cmp.Compare(slices.Index(order, a.Site), slices.Index(order, b.Site))
		})
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
