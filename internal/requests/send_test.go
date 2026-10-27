package requests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// request is one approved request of a library that searches TMDB and sends to a fetcher, and
// what became of it.
type request struct {
	lib    domain.Library
	state  domain.RequestState
	reason string
}

func (r *request) Sending(context.Context, uuid.UUID) (domain.Wanted, domain.Library, bool, error) {
	w := domain.Wanted{Kind: domain.ItemShow, Title: "Breaking Bad", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396"}}
	return w, r.lib, r.state == domain.RequestApproved, nil
}

func (r *request) SentRequest(_ context.Context, id uuid.UUID, state domain.RequestState, reason string) (store.Request, bool, error) {
	r.state, r.reason = state, reason
	return store.Request{ID: id, Title: "Breaking Bad", State: state, Reason: reason}, true, nil
}

// tmdb knows the show's TheTVDB id.
type tmdb struct{}

func (tmdb) Info() provider.Info { return provider.Info{ID: domain.SourceTMDB} }

func (tmdb) Match(context.Context, domain.Locale, domain.ItemKind, provider.Hints) (string, error) {
	return "", nil
}

func (tmdb) Describe(_ context.Context, _ domain.Locale, _ domain.ItemKind, id string, _ domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	if id != "1396" {
		return domain.Metadata{}, nil, provider.ErrNotFound
	}
	return domain.Metadata{IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396", domain.ProviderTVDB: "81189"}}, nil, nil
}

// fetcher takes what it is asked for, or answers err.
type fetcher struct {
	err   error
	asked domain.Wanted
}

func (*fetcher) Info() provider.Info { return provider.Info{ID: domain.PluginSource("riven")} }

func (f *fetcher) Request(_ context.Context, w domain.Wanted) error {
	f.asked = w
	return f.err
}

type registry map[domain.FieldSource]provider.Provider

func (r registry) Get(_ context.Context, id domain.FieldSource) (provider.Provider, bool, error) {
	p, ok := r[id]
	return p, ok, nil
}

// An approved request is sent with every id its library's search knows the title by, and is sent;
// refused, it fails with the fetcher's words; a fetcher that is down is asked again later; one
// that fetches nothing fails it; a request decided otherwise since is left alone. Whatever came of
// it is told; a request still waiting to be sent again is not.
func TestAnApprovedRequestIsSentToItsLibrarysFetcher(t *testing.T) {
	riven := domain.PluginSource("riven")
	lib := domain.Library{DiscoverSource: domain.SourceTMDB, RequestSource: riven}
	log := slog.New(slog.DiscardHandler)
	server := func() domain.Locale { return domain.Locale{} }
	for _, tc := range []struct {
		name       string
		fetcher    provider.Provider
		err        error
		wantErr    bool
		wantState  domain.RequestState
		wantReason string
	}{
		{name: "taken on", fetcher: &fetcher{}, wantState: domain.RequestSent},
		{name: "refused", fetcher: &fetcher{err: errors.New("plugin riven: 422: no release")}, wantState: domain.RequestFailed, wantReason: "plugin riven: 422: no release"},
		{name: "down", fetcher: &fetcher{err: fmt.Errorf("riven: %w", provider.ErrUnavailable)}, wantErr: true, wantState: domain.RequestApproved},
		{name: "unreached", fetcher: &fetcher{err: fmt.Errorf("riven /graphql: %w", provider.ErrUnreached)}, wantErr: true, wantState: domain.RequestApproved},
		{name: "failing on its side", fetcher: &fetcher{err: fmt.Errorf("radarr: %w", &provider.Refusal{Code: http.StatusBadGateway, Status: "502 Bad Gateway"})}, wantErr: true, wantState: domain.RequestApproved},
		{name: "fetches nothing", fetcher: tmdb{}, wantState: domain.RequestFailed, wantReason: "plugin:riven fetches no titles"},
	} {
		r := &request{lib: lib, state: domain.RequestApproved}
		var told []domain.RequestState
		raise := func(_ context.Context, e domain.Event) {
			if d, ok := e.Details.(domain.RequestDetails); ok && e.Kind == domain.EventRequestChanged {
				told = append(told, d.State)
			}
		}
		err := Send(r, registry{domain.SourceTMDB: tmdb{}, riven: tc.fetcher}, server, raise, log)(t.Context(), uuid.NewV7())
		if (err != nil) != tc.wantErr || r.state != tc.wantState || r.reason != tc.wantReason {
			t.Errorf("%s: %v, %s %q; want %s %q", tc.name, err, r.state, r.reason, tc.wantState, tc.wantReason)
		}
		if want := []domain.RequestState{tc.wantState}; tc.wantErr && told != nil || !tc.wantErr && !slices.Equal(told, want) {
			t.Errorf("%s: told %v", tc.name, told)
		}
		if f, ok := tc.fetcher.(*fetcher); ok && (f.asked.IDs[domain.ProviderTVDB] != "81189" || f.asked.IDs[domain.ProviderTMDB] != "1396") {
			t.Errorf("%s: asked by %v, want TMDB's and TheTVDB's ids", tc.name, f.asked.IDs)
		}
	}
	declined := &request{lib: lib, state: domain.RequestDeclined}
	f := &fetcher{}
	if err := Send(declined, registry{riven: f}, server, func(context.Context, domain.Event) { t.Error("a request decided otherwise is told") }, log)(t.Context(), uuid.NewV7()); err != nil || f.asked.Title != "" {
		t.Errorf("a request decided otherwise: %v, asked %+v", err, f.asked)
	}
}
