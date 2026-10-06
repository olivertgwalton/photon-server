// Package provider is what the server knows titles by beyond their files: metadata providers,
// each saying what it can do and what it needs set, as Jellyfin's metadata providers and Plex's
// agents do. Built-in providers are compiled in, and plugins an admin registers are read from
// Postgres; a library chooses which of them it takes, and in what order.
package provider

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

var (
	// ErrNotConfigured is a provider missing a setting it needs, such as its key: it is passed over.
	ErrNotConfigured = errors.New("provider: not configured")
	// ErrUnavailable is a provider that could not be reached, as a plugin that is down: it is
	// passed over too, so the next source is asked.
	ErrUnavailable = errors.New("provider: unavailable")
	// ErrNotFound is a provider answering 404.
	ErrNotFound = errors.New("provider: not found")
	// ErrUnreached is a request a provider never answered whole: no connection, a timeout, an
	// answer cut short. A built-in provider's fails its job, to be tried again; a plugin's is
	// ErrUnavailable.
	ErrUnreached = errors.New("provider: not reached")
)

// maxAnswer bounds what a provider may answer to one request rather than holding whatever it sends
// in memory; a long show described whole is the largest.
const maxAnswer = 8 << 20

// builtinHTTP is the built-in providers' HTTP client.
var builtinHTTP = &http.Client{Timeout: 30 * time.Second}

// Client asks a provider for JSON, as every provider is asked: after its share of Limit where
// Limits is set, again after a 429 as the provider says (send), and reading at most maxAnswer. An
// error names Name and the address's path, never the address, which may carry a key.
type Client struct {
	Name string
	// HTTP is nil for a built-in provider's.
	HTTP   *http.Client
	Limits kv.Limiter
	Limit  kv.Limit
}

// Refusal is a provider's answer other than 200: its status, and its body for what it says.
type Refusal struct {
	Code   int
	Status string
	Body   []byte
}

func (r *Refusal) Error() string { return r.Status }

func (r *Refusal) Is(target error) bool {
	return target == ErrNotFound && r.Code == http.StatusNotFound
}

// Do sends req and decodes a 200's JSON into out; any other status is a *Refusal.
func (c Client) Do(req *http.Request, out any) error {
	if c.Limits != nil {
		if err := kv.Wait(req.Context(), c.Limits, c.Name, c.Limit); err != nil {
			return err
		}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := send(cmp.Or(c.HTTP, builtinHTTP), req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("%s %s: %w: %w", c.Name, req.URL.Path, ErrUnreached, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	switch {
	case err != nil:
		return fmt.Errorf("%s %s: %w: %w", c.Name, req.URL.Path, ErrUnreached, err)
	case len(data) > maxAnswer:
		return fmt.Errorf("%s %s: answered more than %d bytes", c.Name, req.URL.Path, maxAnswer)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s %s: %w", c.Name, req.URL.Path, &Refusal{Code: resp.StatusCode, Status: resp.Status, Body: data})
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: %w", c.Name, req.URL.Path, err)
	}
	return nil
}

// maxRetryAfter is the longest a provider's asking to be given time is waited out within one
// request; one asking longer fails it, and its job is tried again later.
const maxRetryAfter = 30 * time.Second

// send sends req, and sends it again once a provider answering 429 Too Many Requests says it may,
// so a burst of identifying waits a moment rather than failing titles into their backoff.
func send(hc *http.Client, req *http.Request) (*http.Response, error) {
	var waited time.Duration
	for {
		resp, err := hc.Do(req) //nolint:gosec // a built-in provider's own address, or a plugin's an admin registered
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		wait := retryAfter(resp.Header.Get("Retry-After"))
		if waited += wait; waited > maxRetryAfter {
			return resp, nil
		}
		_ = resp.Body.Close()
		if req.GetBody != nil {
			if req.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

// retryAfter reads a Retry-After header, seconds or a date; a provider that says nothing is given
// a second.
func retryAfter(v string) time.Duration {
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return time.Second
}

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
	Describe(ctx context.Context, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error)
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

// Partial is a provider that implements every capability's methods but answers only some of
// them: a plugin, whose manifest says which.
type Partial interface {
	Answers(c domain.Capability) bool
}

// As answers p as the capability c, implemented by T, where p has it.
func As[T Provider](p Provider, c domain.Capability) (T, bool) {
	t, ok := p.(T)
	if part, partial := p.(Partial); ok && partial {
		ok = part.Answers(c)
	}
	return t, ok
}

// Capabilities answers what p can do.
func Capabilities(p Provider) []domain.Capability {
	out := []domain.Capability{}
	for _, c := range domain.Capabilities() {
		var ok bool
		switch c {
		case domain.CapabilityDescribe:
			_, ok = As[Describer](p, c)
		case domain.CapabilitySearch:
			_, ok = As[Searcher](p, c)
		case domain.CapabilityRate:
			_, ok = As[Rater](p, c)
		case domain.CapabilityPerson:
			_, ok = As[PersonDescriber](p, c)
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// Settings answers a provider's settings as an admin set them.
type Settings func(ctx context.Context) (map[string]string, error)

// pluginsFor is how long the plugins read from Postgres stand before they are read again. A
// plugin registered or removed reaches the node it was asked at at once (Forget), and every other
// node within this, without a read for every title a job describes.
const pluginsFor = 30 * time.Second

// Registry is the providers the server has, in the order they run: the built-in ones, one whose
// match gives another an id to find the title by first, then the registered plugins.
type Registry struct {
	builtin []Provider
	load    func(context.Context) ([]Provider, error)

	mu      sync.Mutex
	plugins []Provider
	readAt  time.Time
}

// NewRegistry is the built-in providers and, where load is not nil, the plugins it reads.
func NewRegistry(load func(context.Context) ([]Provider, error), builtin ...Provider) *Registry {
	return &Registry{builtin: builtin, load: load}
}

// All answers every provider, in the order they run.
func (r *Registry) All(ctx context.Context) ([]Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.load != nil && time.Since(r.readAt) >= pluginsFor {
		plugins, err := r.load(ctx)
		if err != nil {
			return nil, err
		}
		r.plugins, r.readAt = plugins, time.Now()
	}
	return slices.Concat(r.builtin, r.plugins), nil
}

// Forget has the plugins read again at the next call, as one was registered or removed.
func (r *Registry) Forget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readAt = time.Time{}
}

// Get answers a provider by its id.
func (r *Registry) Get(ctx context.Context, id domain.FieldSource) (Provider, bool, error) {
	all, err := r.All(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, p := range all {
		if p.Info().ID == id {
			return p, true, nil
		}
	}
	return nil, false, nil
}

// DescribePerson asks each provider that describes people, in order, what it knows of someone;
// false where none knows them.
func (r *Registry) DescribePerson(ctx context.Context, ids map[domain.Provider]string) (domain.Person, bool, error) {
	all, err := r.All(ctx)
	if err != nil {
		return domain.Person{}, false, err
	}
	var errs []error
	for _, p := range all {
		d, ok := As[PersonDescriber](p, domain.CapabilityPerson)
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

// Date reads a provider's YYYY-MM-DD date; one it does not give, or gives malformed, is the zero
// time.
func Date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

// Year is the year of a date, or zero for the zero time.
func Year(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return t.Year()
}
