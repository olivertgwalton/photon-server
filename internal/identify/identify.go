// Package identify matches films and shows to metadata providers.
package identify

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Handler asks each provider a film's or show's library asks for anything, in the registry's
// order, what it knows: a describer matches the title and records what it says about it and, for a
// show, its seasons and episodes, and a rater records its ratings; the library's ranking for each
// kind of item decides whose values and pictures stand. A title with no confident match is left as its files and NFO describe it, and a provider
// not configured or not reachable is passed over. Each provider is asked in the title's library's
// locale, def where it leaves anything unsaid; def is the server's. raise tells the title was
// described again, once the pictures it shows first are fetched into pictures, so a client that
// looks again finds them there.
func Handler(st *store.Store, providers *provider.Registry, pictures *artwork.Cache, def domain.Locale, raise func(context.Context, domain.Event), log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sub, ok, err := st.IdentifySubject(ctx, id)
		if err != nil || !ok || sub.Unmatched {
			return err
		}
		loc := sub.Locale.Or(def)
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
				if err := describe(ctx, st, d, loc, def, id, &sub, log); err != nil {
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
		fetch(ctx, st, pictures, id, log)
		raise(ctx, domain.Event{Kind: domain.EventTitleUpdated, Item: id})
		return nil
	}
}

func describe(ctx context.Context, st *store.Store, d provider.Describer, loc, def domain.Locale, id uuid.UUID, sub *store.Subject, log *slog.Logger) error {
	match, err := d.Match(ctx, loc, sub.Kind, provider.Hints{Title: sub.Title, Year: sub.Year, IDs: sub.IDs})
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
	m, seasons, err := d.Describe(ctx, loc, sub.Kind, match, domain.SeasonRequest{Numbers: sub.Seasons, Order: sub.Order})
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
	m.Certificate = loc.Qualified(m.Certificate, def)
	// A library giving original titles names a film or show as it was first named, sorted by it.
	if sub.Titles == domain.TitlesOriginal && m.OriginalTitle != "" {
		m.Title, m.SortTitle = m.OriginalTitle, ""
	}
	for n, season := range seasons {
		season.Metadata.Certificate = loc.Qualified(season.Metadata.Certificate, def)
		for e, episode := range season.Episodes {
			episode.Certificate = loc.Qualified(episode.Certificate, def)
			season.Episodes[e] = episode
		}
		seasons[n] = season
	}
	return st.SaveIdentity(ctx, id, d.Info().ID, m, seasons)
}

// fetch fetches the pictures a title shows first. A title is described without them, so one not
// fetched is reported rather than failing the job; it is fetched when it is first asked for.
func fetch(ctx context.Context, st *store.Store, pictures *artwork.Cache, id uuid.UUID, log *slog.Logger) {
	unfetched, err := st.TitleUnfetched(ctx, id)
	n := 0
	if err == nil {
		n, err = pictures.Fetch(ctx, unfetched)
	}
	if ctx.Err() == nil && (err != nil || n < len(unfetched)) {
		log.WarnContext(ctx, "pictures not fetched", slog.Int("pictures", len(unfetched)-n), slog.Any("err", err))
	}
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
