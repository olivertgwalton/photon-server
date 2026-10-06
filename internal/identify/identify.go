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

// Handler asks each provider a film's or show's library asks for anything, in the registry's
// order, what it knows: a describer matches the title and records what it says about it and, for a
// show, its seasons and episodes, and a rater records its ratings; the library's ranking for each
// kind of item decides whose values and pictures stand. A title with no confident match is left as its files and NFO describe it, and a provider
// not configured or not reachable is passed over. raise tells the title was described again.
func Handler(st *store.Store, providers *provider.Registry, raise func(context.Context, domain.Event), log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sub, ok, err := st.IdentifySubject(ctx, id)
		if err != nil || !ok {
			return err
		}
		all, err := providers.All(ctx)
		if err != nil {
			return err
		}
		for _, p := range all {
			info := p.Info()
			if !slices.Contains(sub.Sources, info.ID) || !slices.Contains(info.Kinds, sub.Kind) {
				continue
			}
			log := log.With(slog.String("provider", string(info.ID)), slog.String("title", sub.Title))
			if d, ok := provider.As[provider.Describer](p, domain.CapabilityDescribe); ok {
				if err := describe(ctx, st, d, id, &sub, log); err != nil {
					return err
				}
			}
			if r, ok := provider.As[provider.Rater](p, domain.CapabilityRate); ok {
				rate(ctx, st, r, id, sub, log)
			}
		}
		if err := st.Identified(ctx, id); err != nil {
			return err
		}
		raise(ctx, domain.Event{Kind: domain.EventTitleUpdated, Item: id})
		return nil
	}
}

func describe(ctx context.Context, st *store.Store, d provider.Describer, id uuid.UUID, sub *store.Subject, log *slog.Logger) error {
	match, err := d.Match(ctx, sub.Kind, provider.Hints{Title: sub.Title, Year: sub.Year, IDs: sub.IDs})
	if passedOver(ctx, err, log) {
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
	if passedOver(ctx, err, log) {
		return nil
	}
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

// passedOver is a provider that is not configured, or cannot be reached, as a plugin that is down:
// the next source is asked rather than the job failing.
func passedOver(ctx context.Context, err error, log *slog.Logger) bool {
	if errors.Is(err, provider.ErrUnavailable) {
		log.WarnContext(ctx, "provider passed over", slog.Any("err", err))
		return true
	}
	return errors.Is(err, provider.ErrNotConfigured)
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
