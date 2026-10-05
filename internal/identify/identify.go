// Package identify matches films and shows to metadata providers.
package identify

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Handler asks each provider a film's or show's library takes, in the registry's order, what it
// knows: a describer matches the title and records what it says about it and, for a show, its
// seasons and episodes, and a rater records its ratings; the library's order decides whose values
// stand. A title with no confident match is left as its files and NFO describe it.
func Handler(st *store.Store, providers *provider.Registry, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sub, ok, err := st.IdentifySubject(ctx, id)
		if err != nil || !ok {
			return err
		}
		for _, p := range providers.All() {
			info := p.Info()
			if !slices.Contains(sub.Sources, info.ID) || !slices.Contains(info.Kinds, sub.Kind) {
				continue
			}
			log := log.With(slog.String("provider", string(info.ID)), slog.String("title", sub.Title))
			if d, ok := p.(provider.Describer); ok {
				if err := describe(ctx, st, d, id, &sub, log); err != nil {
					return err
				}
			}
			if r, ok := p.(provider.Rater); ok {
				rate(ctx, st, r, id, sub, log)
			}
		}
		return st.Identified(ctx, id)
	}
}

func describe(ctx context.Context, st *store.Store, d provider.Describer, id uuid.UUID, sub *store.Subject, log *slog.Logger) error {
	match, err := d.Match(ctx, sub.Kind, provider.Hints{Title: sub.Title, Year: sub.Year, IDs: sub.IDs})
	if errors.Is(err, provider.ErrNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	if match == "" {
		log.InfoContext(ctx, "no confident match", slog.Int("year", sub.Year))
		return nil
	}
	m, seasons, err := d.Describe(ctx, sub.Kind, match, domain.SeasonRequest{Numbers: sub.Seasons, Order: sub.Order})
	if err != nil {
		return err
	}
	// The next provider may find the title by an id this one gave.
	for p, v := range m.IDs {
		if _, ok := sub.IDs[p]; !ok {
			sub.IDs[p] = v
		}
	}
	return st.SaveIdentity(ctx, id, d.Info().ID, m, seasons)
}

// rate records a title's ratings. Ratings are worth less than the match before them, so a rater
// that fails, as one past its daily allowance does, is reported rather than failing the job and
// asking every provider again.
func rate(ctx context.Context, st *store.Store, r provider.Rater, id uuid.UUID, sub store.Subject, log *slog.Logger) {
	ratings, err := r.Ratings(ctx, sub.Kind, sub.IDs)
	if errors.Is(err, provider.ErrNotConfigured) {
		return
	}
	if err == nil {
		err = st.SaveRatings(ctx, id, r.Info().ID, ratings)
	}
	if err != nil && ctx.Err() == nil {
		log.WarnContext(ctx, "ratings not read", slog.Any("err", err))
	}
}
