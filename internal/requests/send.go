// Package requests sends the titles profiles asked for, once approved, to the provider their
// library's requests go to, as Overseerr hands them to Radarr and Sonarr.
package requests

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type sending interface {
	Sending(ctx context.Context, id uuid.UUID) (domain.Wanted, domain.Library, bool, error)
	SentRequest(ctx context.Context, id uuid.UUID, state domain.RequestState, reason string) (store.Request, bool, error)
}

type providers interface {
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
}

// Send is the job that sends an approved request to the provider its library's requests go to,
// by every id the library's search knows the title by, in the library's locale or else the
// server's. A provider that is down is asked again later; one that refuses fails the request with
// its words. raise tells what came of it.
func Send(st sending, ps providers, server func() domain.Locale, raise func(context.Context, domain.Event), log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sent := func(state domain.RequestState, reason string) error {
			r, changed, err := st.SentRequest(ctx, id, state, reason)
			if changed {
				raise(ctx, r.Event(domain.EventRequestChanged))
			}
			return err
		}
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
			return sent(domain.RequestFailed, string(lib.RequestSource)+" fetches no titles")
		}
		err = r.Request(ctx, w)
		switch {
		case down(err), ctx.Err() != nil:
			return err
		case err != nil:
			return sent(domain.RequestFailed, err.Error())
		}
		return sent(domain.RequestSent, "")
	}
}

// down is a provider that could not take a request now, which is sent again later: one that never
// answered, failed on its side, or is a plugin that is down.
func down(err error) bool {
	refusal, refused := errors.AsType[*provider.Refusal](err)
	return errors.Is(err, provider.ErrUnavailable) || errors.Is(err, provider.ErrUnreached) ||
		refused && refusal.Code >= http.StatusInternalServerError
}

// known is every id the library's search source knows the title by, the one it was found under
// first: one that knows shows by TheTVDB's ids, as Sonarr and riven do, is sent that too. A source
// that cannot say leaves the title with the id it was found under.
func known(ctx context.Context, ps providers, lib domain.Library, loc domain.Locale, w domain.Wanted, log *slog.Logger) (map[domain.Provider]string, error) {
	m, asked, err := description(ctx, ps, lib, loc, w.Kind, w.IDs[domain.Provider(lib.DiscoverSource)])
	switch {
	case !asked:
		return w.IDs, err
	case down(err):
		return nil, err
	case err != nil:
		log.WarnContext(ctx, "a requested title's ids could not be read", slog.String("provider", string(lib.DiscoverSource)), slog.Any("err", err))
		return w.IDs, nil
	}
	ids := map[domain.Provider]string{}
	maps.Copy(ids, m.IDs)
	maps.Copy(ids, w.IDs)
	return ids, nil
}

// description is the title as the library's search source describes it, and whether it was
// asked: a source that is missing or cannot describe is not.
func description(ctx context.Context, ps providers, lib domain.Library, loc domain.Locale, kind domain.ItemKind, id string) (domain.Metadata, bool, error) {
	p, ok, err := ps.Get(ctx, lib.DiscoverSource)
	if err != nil || !ok {
		return domain.Metadata{}, false, err
	}
	d, describes := provider.As[provider.Describer](p, domain.CapabilityDescribe)
	if !describes {
		return domain.Metadata{}, false, nil
	}
	m, _, err := d.Describe(ctx, loc, kind, id, domain.SeasonRequest{Scope: domain.SeasonsNumbered})
	return m, true, err
}
