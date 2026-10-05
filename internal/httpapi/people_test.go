package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var weaver = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a9")

// fakePeople knows Sigourney Weaver, never described, credited on one film.
type fakePeople struct{ described *domain.Person }

func (f *fakePeople) Person(_ context.Context, id uuid.UUID) (store.PersonPage, error) {
	if id != weaver {
		return store.PersonPage{}, store.ErrNotFound
	}
	p := store.PersonPage{ID: weaver, Name: "Sigourney Weaver", IDs: map[domain.Provider]string{domain.ProviderTMDB: "10205"}}
	if f.described != nil {
		p.Biography, p.DescribedAt = f.described.Biography, time.Now()
	}
	return p, nil
}

func (f *fakePeople) PersonCredits(context.Context, uuid.UUID, uuid.UUID) ([]store.PersonCredit, error) {
	return []store.PersonCredit{{Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Alien"}, Kind: domain.CreditActor, Role: "Ripley"}}, nil
}

func (f *fakePeople) DescribePerson(_ context.Context, _ uuid.UUID, d domain.Person) error {
	f.described = &d
	return nil
}

func (f *fakePeople) SearchPeople(_ context.Context, text string, _ int) ([]store.PersonRef, error) {
	if strings.HasPrefix("sigourney weaver", strings.ToLower(text)) {
		return []store.PersonRef{{ID: weaver, Name: "Sigourney Weaver"}}, nil
	}
	return []store.PersonRef{}, nil
}

type describer struct {
	asked int
	fail  bool
}

func (d *describer) DescribePerson(context.Context, map[domain.Provider]string) (domain.Person, bool, error) {
	d.asked++
	if d.fail {
		return domain.Person{}, false, errors.New("tmdb is down")
	}
	return domain.Person{Name: "Sigourney Weaver", Biography: "An actor."}, true, nil
}

func TestAPersonsPageSaysWhoTheyAreOnceAMonth(t *testing.T) {
	get := func(api *API, id uuid.UUID) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/people/"+id.String(), nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	down := &describer{fail: true}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, People: &fakePeople{}, PersonDescriber: down})
	if rec := get(api, weaver); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"credits":[{"kind":"actor","role":"Ripley","id"`) {
		t.Errorf("with the provider down: %d %s, want the page as known", rec.Code, rec.Body)
	}
	up := &describer{}
	api = New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, People: &fakePeople{}, PersonDescriber: up})
	for range 2 {
		if rec := get(api, weaver); !strings.Contains(rec.Body.String(), `"biography":"An actor."`) {
			t.Errorf("described: %s", rec.Body)
		}
	}
	if up.asked != 1 {
		t.Errorf("the provider was asked %d times, want once", up.asked)
	}
	if rec := get(api, uuid.NewV7()); rec.Code != http.StatusNotFound {
		t.Errorf("no one: %d, want 404", rec.Code)
	}
}
