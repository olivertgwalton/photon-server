// Package provider is what the server knows titles by beyond their files: metadata providers as
// plugins, each saying what it can do and what it needs set, as Jellyfin's metadata providers and
// Plex's agents do. Built-in providers are compiled in; a library chooses which of them it takes,
// and in what order.
package provider

import (
	"context"
	"errors"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ErrNotConfigured is a provider missing a setting it needs, such as its key: it is passed over.
var ErrNotConfigured = errors.New("provider: not configured")

// Info is what a provider is and what an admin may set of it.
type Info struct {
	ID   domain.FieldSource
	Name string
	// Kinds are the titles it knows: films, shows or both.
	Kinds    []domain.ItemKind
	Settings []Setting
}

// Setting is something an admin sets for a provider.
type Setting struct {
	Key  string
	Name string
	// Secret is never sent back once set.
	Secret   bool
	Required bool
}

// Provider is a source of what is known about titles.
type Provider interface {
	Info() Info
}

// Hints are what a title is matched by: its name and year, and the ids it already carries.
type Hints struct {
	Title string
	Year  int
	IDs   map[domain.Provider]string
}

// Describer matches a title on its provider and says what the provider knows of it and, for a
// show, of the seasons asked for.
type Describer interface {
	Provider
	// Match answers the title's id on the provider, empty for no confident match.
	Match(ctx context.Context, kind domain.ItemKind, h Hints) (string, error)
	Describe(ctx context.Context, kind domain.ItemKind, id string, seasons []int) (domain.Metadata, map[int]domain.SeasonMetadata, error)
}

// Searcher lists what a provider has by a name, for an admin choosing the match by hand.
type Searcher interface {
	Provider
	Candidates(ctx context.Context, kind domain.ItemKind, title string, year int) ([]domain.Candidate, error)
}

// Rater says what sites' readers and critics make of a title, found by the ids it carries.
type Rater interface {
	Provider
	Ratings(ctx context.Context, kind domain.ItemKind, ids map[domain.Provider]string) ([]domain.Rating, error)
}

// PersonDescriber says what a provider knows of someone it credits, found by their ids.
type PersonDescriber interface {
	Provider
	DescribePerson(ctx context.Context, ids map[domain.Provider]string) (domain.Person, error)
}

// Settings answers a provider's settings as an admin set them.
type Settings func(ctx context.Context) (map[string]string, error)

// Registry is the providers the server has, in the order they run: one whose match gives
// another an id to find the title by comes first.
type Registry struct {
	all []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{all: providers}
}

// All answers every provider, in the order they run.
func (r *Registry) All() []Provider { return r.all }

// Get answers a provider by its id.
func (r *Registry) Get(id domain.FieldSource) (Provider, bool) {
	for _, p := range r.all {
		if p.Info().ID == id {
			return p, true
		}
	}
	return nil, false
}

// DescribePerson asks each provider that describes people, in order, what it knows of someone;
// false where none knows them.
func (r *Registry) DescribePerson(ctx context.Context, ids map[domain.Provider]string) (domain.Person, bool, error) {
	var errs []error
	for _, p := range r.all {
		d, ok := p.(PersonDescriber)
		if !ok {
			continue
		}
		person, err := d.DescribePerson(ctx, ids)
		if err == nil {
			return person, true, nil
		}
		errs = append(errs, err)
	}
	return domain.Person{}, false, errors.Join(errs...)
}
