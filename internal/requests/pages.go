package requests

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type describing interface {
	Askable(ctx context.Context, profile, id uuid.UUID) (store.Askable, error)
	Library(ctx context.Context, id uuid.UUID) (domain.Library, error)
	KeepDescription(ctx context.Context, id uuid.UUID, m domain.Metadata) error
	RequestablePage(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
}

// Pages draws the titles a search found that a profile may ask for as a title's page is drawn,
// described by the provider whose search found them the first time one is opened, in its
// library's locale or else the server's. A provider that cannot say leaves the page what the
// search said.
type Pages struct {
	st        describing
	providers providers
	server    func() domain.Locale
	log       *slog.Logger
}

func NewPages(st describing, ps providers, server func() domain.Locale, log *slog.Logger) *Pages {
	return &Pages{st: st, providers: ps, server: server, log: log}
}

// Page answers a find a profile may ask for as a title's page, or store.ErrNotFound.
func (p *Pages) Page(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error) {
	a, err := p.st.Askable(ctx, profile, id)
	if err != nil {
		return store.TitlePage{}, err
	}
	if !a.Described {
		if err := p.describe(ctx, a); err != nil {
			p.log.WarnContext(ctx, "a title to ask for was not described", slog.String("title", id.String()), slog.Any("err", err))
		}
	}
	return p.st.RequestablePage(ctx, profile, id)
}

func (p *Pages) describe(ctx context.Context, a store.Askable) error {
	lib, err := p.st.Library(ctx, a.Library)
	if err != nil {
		return err
	}
	m, asked, err := description(ctx, p.providers, lib, lib.Locale.Or(p.server()), a.Kind, a.Value)
	if !asked || err != nil {
		return err
	}
	return p.st.KeepDescription(ctx, a.ID, m)
}
