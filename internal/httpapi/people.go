package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type people interface {
	Person(ctx context.Context, id uuid.UUID) (store.PersonPage, error)
	PersonCredits(ctx context.Context, profile, person uuid.UUID) ([]store.PersonCredit, error)
	DescribePerson(ctx context.Context, id uuid.UUID, d domain.Person) error
	SearchPeople(ctx context.Context, text string, limit int) ([]store.PersonRef, error)
}

type personDescriber interface {
	DescribePerson(ctx context.Context, ids map[domain.Provider]string) (domain.Person, bool, error)
}

// describedFor is how long what a provider said of someone stands before it is asked again.
const describedFor = 30 * 24 * time.Hour

type creditJSON struct {
	Kind domain.CreditKind `json:"kind"`
	Role string            `json:"role,omitzero"`
	cardJSON
}

type personJSON struct {
	store.PersonPage
	Credits []creditJSON `json:"credits"`
}

// person answers someone's page: who they are, as a provider says the first time it is opened and
// monthly after, and their films and shows here, the newest first.
func (a *API) person(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	p, err := a.svc.People.Person(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if time.Since(p.DescribedAt) > describedFor && len(p.IDs) > 0 {
		d, ok, err := a.svc.PersonDescriber.DescribePerson(r.Context(), p.IDs)
		if ok {
			err = a.svc.People.DescribePerson(r.Context(), id, d)
		}
		if err == nil && ok {
			p, err = a.svc.People.Person(r.Context(), id)
		}
		// Who they are is worth less than the page: a provider that fails is passed over.
		if err != nil && !errors.Is(err, context.Canceled) {
			a.logger.WarnContext(r.Context(), "person not described", slog.Any("err", err))
		}
	}
	credits, err := a.svc.People.PersonCredits(r.Context(), sessionOf(r).Profile.ID, id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := personJSON{PersonPage: p, Credits: []creditJSON{}}
	for _, c := range credits {
		out.Credits = append(out.Credits, creditJSON{Kind: c.Kind, Role: c.Role, cardJSON: cardsJSON([]store.Card{c.Card})[0]})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}
