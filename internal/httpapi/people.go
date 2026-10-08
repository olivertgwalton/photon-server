package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type people interface {
	Person(ctx context.Context, id uuid.UUID) (store.PersonPage, error)
	PersonCredits(ctx context.Context, profile, person uuid.UUID) ([]store.PersonCredit, error)
	DescribePerson(ctx context.Context, id uuid.UUID, d domain.Person) error
	SearchPeople(ctx context.Context, text string, offset, limit int) ([]store.PersonRef, int64, error)
}

type personDescriber interface {
	DescribePerson(ctx context.Context, loc domain.Locale, ids map[domain.Provider]string) (domain.Person, bool, error)
}

// describedFor is how long what a provider said of someone stands before it is asked again.
const describedFor = 30 * 24 * time.Hour

type creditJSON struct {
	Credit domain.CreditKind `json:"credit"`
	Role   string            `json:"role,omitzero"`
	cardJSON
}

type personJSON struct {
	ID         uuid.UUID                  `json:"id"`
	Name       string                     `json:"name"`
	Photo      uuid.UUID                  `json:"photo,omitzero"`
	Blurhashes store.Blurhashes           `json:"blurhashes,omitzero"`
	Biography  string                     `json:"biography,omitzero"`
	Born       domain.Date                `json:"born,omitzero"`
	Died       domain.Date                `json:"died,omitzero"`
	Birthplace string                     `json:"birthplace,omitzero"`
	IDs        map[domain.Provider]string `json:"ids,omitzero"`
	Credits    []creditJSON               `json:"credits"`
}

// person answers someone's page: who they are, as a provider says the first time it is opened and
// monthly after, and their films and shows here, the newest first.
func (a *API) person(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := a.svc.People.Person(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if time.Since(p.DescribedAt) > describedFor && len(p.IDs) > 0 {
		d, ok, err := a.svc.PersonDescriber.DescribePerson(r.Context(), domain.Locale{Language: p.Language}.Or(a.svc.Identity.Locale()), p.IDs)
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
	credits, err := a.svc.People.PersonCredits(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := personJSON{
		ID: p.ID, Name: p.Name, Photo: p.Photo, Blurhashes: p.Blurhashes, Biography: p.Biography, Born: p.Born,
		Died: p.Died, Birthplace: p.Birthplace, IDs: p.IDs, Credits: []creditJSON{},
	}
	for _, c := range credits {
		out.Credits = append(out.Credits, creditJSON{Credit: c.Kind, Role: c.Role, cardJSON: cardOf(c.Card)})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func (a *API) peopleRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/people/{id}", access: signedIn, summary: "Someone's page and their titles here",
			status: http.StatusOK, reply: personJSON{}, handle: a.person,
		},
	}
}
