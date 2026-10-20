// Package requests sends the titles profiles asked for, once approved, to the provider their
// library's requests go to, as Overseerr hands them to Radarr and Sonarr.
package requests

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type sending interface {
	Sending(ctx context.Context, id uuid.UUID) (domain.Wanted, domain.Library, bool, error)
	SentRequest(ctx context.Context, id uuid.UUID, state domain.RequestState, reason string) error
}

type providers interface {
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
}

// Send is the job that sends an approved request to the provider its library's requests go to,
// by every id the library's search knows the title by, in the library's locale or else the
// server's. A provider that is down is asked again later; one that refuses fails the request with
// its words.
func Send(st sending, ps providers, server func() domain.Locale, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		w, lib, ok, err := st.Sending(ctx, id)
		if err != nil || !ok {
			return err
		}
		if w.IDs, err = known(ctx, ps, lib, lib.Locale.Or(server()), w, log); err != nil {
			return err
		}
		p, found, err := ps.Get(ctx, lib.RequestSource)
		if err != nil {
			return err
		}
		r, fetches := provider.As[provider.Requester](p, domain.CapabilityRequest)
		if !found || !fetches {
			return st.SentRequest(ctx, id, domain.RequestFailed, string(lib.RequestSource)+" fetches no titles")
		}
		err = r.Request(ctx, w)
		switch {
		case errors.Is(err, provider.ErrUnavailable), ctx.Err() != nil:
			return err
		case err != nil:
			return st.SentRequest(ctx, id, domain.RequestFailed, err.Error())
		}
		return st.SentRequest(ctx, id, domain.RequestSent, "")
	}
}

// known is every id the library's search source knows the title by, the one it was found under
// first: one that knows shows by TheTVDB's ids, as Sonarr and riven do, is sent that too. A source
// that cannot say leaves the title with the id it was found under.
func known(ctx context.Context, ps providers, lib domain.Library, loc domain.Locale, w domain.Wanted, log *slog.Logger) (map[domain.Provider]string, error) {
	p, ok, err := ps.Get(ctx, lib.DiscoverSource)
	if err != nil || !ok {
		return w.IDs, err
	}
	d, describes := provider.As[provider.Describer](p, domain.CapabilityDescribe)
	if !describes {
		return w.IDs, nil
	}
	m, _, err := d.Describe(ctx, loc, w.Kind, w.IDs[domain.Provider(lib.DiscoverSource)], domain.SeasonRequest{Scope: domain.SeasonsNumbered})
	if errors.Is(err, provider.ErrUnavailable) {
		return nil, err
	}
	if err != nil {
		log.WarnContext(ctx, "a requested title's ids could not be read", slog.String("provider", string(lib.DiscoverSource)), slog.Any("err", err))
		return w.IDs, nil
	}
	ids := maps.Clone(m.IDs)
	if ids == nil {
		ids = map[domain.Provider]string{}
	}
	maps.Copy(ids, w.IDs)
	return ids, nil
}
