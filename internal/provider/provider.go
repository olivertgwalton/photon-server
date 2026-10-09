// Package provider is what the server knows titles by beyond their files: metadata providers,
// each saying what it can do and what it needs set, as Jellyfin's metadata providers and Plex's
// agents do. Built-in providers are compiled in, and plugins an admin registers are read from
// Postgres; a library chooses which of them it takes, and in what order.
package provider

import (
	"bytes"
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
	"strings"
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
	// ErrQuota is an account that has used what a provider lets it have today: what is left is
	// asked for tomorrow.
	ErrQuota = errors.New("provider: today's quota is used")
)

// maxAnswer bounds what a provider may answer to one request rather than holding whatever it sends
// in memory; a long show described whole is the largest.
const maxAnswer = 8 << 20

// builtinHTTP is the built-in providers' HTTP client.
var builtinHTTP = &http.Client{Timeout: 30 * time.Second}

// Client asks a provider for JSON, as every provider is asked: after its share of Limit where
// Limits is set, again after a 429 as the provider says (send), and reading at most maxAnswer. A
// request goes to Base and nowhere else: what a caller names is a path under it. An error names
// Name and the path, never the address, which may carry a key.
type Client struct {
	Name string
	// Base is the provider's http or https address: a built-in provider's own, or the one an
	// admin registered for a plugin or a server to import from.
	Base string
	// HTTP is nil for a built-in provider's.
	HTTP   *http.Client
	Limits kv.Limiter
	Limit  kv.Limit
}

// Request is what a provider is asked: a path under its Client's Base, already escaped, and what
// goes with it. Body, where it is not nil, is sent as a form where it is url.Values, as OAuth
// requests are (RFC 6749), and as JSON otherwise.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   any
}

// Refusal is a provider's answer other than a 2xx: its status, and its body for what it says.
type Refusal struct {
	Code   int
	Status string
	Body   []byte
}

func (r *Refusal) Error() string { return r.Status }

func (r *Refusal) Is(target error) bool {
	return target == ErrNotFound && r.Code == http.StatusNotFound
}

// Do sends r and decodes a 2xx's JSON into out; any other status is a *Refusal.
func (c Client) Do(ctx context.Context, r Request, out any) error {
	r.Header = r.Header.Clone()
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.Header.Set("Accept", "application/json")
	data, err := c.Bytes(ctx, r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: %w", c.Name, r.Path, err)
	}
	return nil
}

// Bytes sends r and answers a 2xx's body as it is, as a write is answered 201 Created; any other
// status is a *Refusal.
func (c Client) Bytes(ctx context.Context, r Request) ([]byte, error) {
	req, err := c.request(ctx, r)
	if err != nil {
		return nil, err
	}
	if c.Limits != nil {
		if err := kv.Wait(ctx, c.Limits, c.Name, c.Limit); err != nil {
			return nil, err
		}
	}
	resp, err := send(cmp.Or(c.HTTP, builtinHTTP), req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s %s: %w: %w", c.Name, r.Path, ErrUnreached, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s %s: %w: %w", c.Name, r.Path, ErrUnreached, err)
	case len(data) > maxAnswer:
		return nil, fmt.Errorf("%s %s: answered more than %d bytes", c.Name, r.Path, maxAnswer)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("%s %s: %w", c.Name, r.Path, &Refusal{Code: resp.StatusCode, Status: resp.Status, Body: data})
	}
	return data, nil
}

// request makes r an HTTP request to Base, refusing a Base that is not an http or https address
// of a host, which no request is sent to.
func (c Client) request(ctx context.Context, r Request) (*http.Request, error) {
	base, err := url.Parse(c.Base)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("%s: %w: its address is not http or https of a host", c.Name, ErrNotConfigured)
	}
	target := base.JoinPath(r.Path)
	target.RawQuery = r.Query.Encode()
	var body io.Reader
	var contentType string
	switch b := r.Body.(type) {
	case nil:
	case url.Values:
		body, contentType = strings.NewReader(b.Encode()), "application/x-www-form-urlencoded"
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		body, contentType = bytes.NewReader(data), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if r.Header != nil {
		req.Header = r.Header.Clone()
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// maxRetryAfter is the longest a provider's asking to be given time is waited out within one
// request; one asking longer fails it, and its job is tried again later.
const maxRetryAfter = 30 * time.Second

// send sends req, and sends it again once a provider answering 429 Too Many Requests says it may,
// so a burst of identifying waits a moment rather than failing titles into their backoff.
func send(hc *http.Client, req *http.Request) (*http.Response, error) {
	var waited time.Duration
	for {
		resp, err := hc.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		wait := retryAfter(resp.Header.Get("Retry-After"))
		if waited += wait; waited > maxRetryAfter {
			return resp, nil
		}
		if err := resp.Body.Close(); err != nil {
			return nil, err
		}
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
	Match(ctx context.Context, loc domain.Locale, kind domain.ItemKind, h Hints) (string, error)
	Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error)
}

// Searcher lists what a provider has by a name, for an admin choosing the match by hand.
type Searcher interface {
	Provider
	Candidates(ctx context.Context, loc domain.Locale, kind domain.ItemKind, title string, year int) ([]domain.Candidate, error)
}

// Rater says what sites' readers and critics make of a title, found by the ids it carries.
type Rater interface {
	Provider
	Ratings(ctx context.Context, kind domain.ItemKind, ids map[domain.Provider]string) ([]domain.Rating, error)
}

// PersonDescriber says what a provider knows of someone it credits, found by their ids.
type PersonDescriber interface {
	Provider
	DescribePerson(ctx context.Context, loc domain.Locale, ids map[domain.Provider]string) (domain.Person, error)
}

// Lister reads a list kept on a provider, its titles in its order, as Kometa's list builders do.
type Lister interface {
	Provider
	List(ctx context.Context, id string) ([]domain.Listed, error)
}

// Streamer streams films and episodes: the copies it offers of one, best first.
type Streamer interface {
	Provider
	Streams(ctx context.Context, title domain.Streamed) ([]domain.Offer, error)
}

// Subtitler finds subtitles for a title, and fetches one as SubRip, as Plex's and Jellyfin's
// subtitle search do.
type Subtitler interface {
	Provider
	SearchSubtitles(ctx context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error)
	FetchSubtitle(ctx context.Context, id string) ([]byte, error)
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
		case domain.CapabilityList:
			_, ok = As[Lister](p, c)
		case domain.CapabilityStream:
			_, ok = As[Streamer](p, c)
		case domain.CapabilitySubtitles:
			_, ok = As[Subtitler](p, c)
		case domain.CapabilitySegments:
			_, ok = As[Segmenter](p, c)
		case domain.CapabilityEvents, domain.CapabilityPages:
			// Events are sent through the webhook queue, and pages are visited, not asked for: only a
			// plugin has them.
			part, partial := p.(Partial)
			ok = partial && part.Answers(c)
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

// ErrNoLister is a provider that keeps no lists, or no such provider.
var ErrNoLister = errors.New("the provider keeps no lists")

// List reads a list kept on the provider source, its titles in its order.
func (r *Registry) List(ctx context.Context, source domain.FieldSource, id string) ([]domain.Listed, error) {
	p, ok, err := r.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	lister, isLister := As[Lister](p, domain.CapabilityList)
	if !ok || !isLister {
		return nil, ErrNoLister
	}
	return lister.List(ctx, id)
}

// ErrNoStreamer is a provider that streams nothing, or no such provider.
var ErrNoStreamer = errors.New("the provider streams nothing")

// Streams asks the provider source for the copies it offers of a film or an episode, best first.
func (r *Registry) Streams(ctx context.Context, source domain.FieldSource, title domain.Streamed) ([]domain.Offer, error) {
	p, ok, err := r.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	streamer, isStreamer := As[Streamer](p, domain.CapabilityStream)
	if !ok || !isStreamer {
		return nil, ErrNoStreamer
	}
	return streamer.Streams(ctx, title)
}

// SearchSubtitles asks every provider that finds subtitles for those of a title, those made for
// its release first, then the most fetched; a provider that fails leaves out what it has.
func (r *Registry) SearchSubtitles(ctx context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	all, err := r.All(ctx)
	if err != nil {
		return nil, err
	}
	var found []domain.FoundSubtitle
	var errs []error
	asked := 0
	for _, p := range all {
		s, ok := As[Subtitler](p, domain.CapabilitySubtitles)
		if !ok {
			continue
		}
		asked++
		got, err := s.SearchSubtitles(ctx, q)
		found = append(found, got...)
		errs = append(errs, err)
	}
	if asked == 0 {
		return nil, ErrNoSubtitler
	}
	slices.SortStableFunc(found, func(a, b domain.FoundSubtitle) int {
		if a.ForRelease != b.ForRelease {
			if a.ForRelease {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.Downloads, a.Downloads)
	})
	if len(found) == 0 {
		return nil, errors.Join(errs...)
	}
	return found, nil
}

// ErrNoSubtitler is a server with no provider that finds subtitles, or a subtitle from none.
var ErrNoSubtitler = errors.New("no provider finds subtitles")

// FetchSubtitle fetches a subtitle a provider found, as SubRip.
func (r *Registry) FetchSubtitle(ctx context.Context, source domain.FieldSource, id string) ([]byte, error) {
	p, ok, err := r.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	s, isSubtitler := As[Subtitler](p, domain.CapabilitySubtitles)
	if !ok || !isSubtitler {
		return nil, ErrNoSubtitler
	}
	return s.FetchSubtitle(ctx, id)
}

// DescribePerson asks each provider that describes people, in order, what it knows of someone;
// false where none knows them.
func (r *Registry) DescribePerson(ctx context.Context, loc domain.Locale, ids map[domain.Provider]string) (domain.Person, bool, error) {
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
		person, err := d.DescribePerson(ctx, loc, ids)
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
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Number reads a provider's number; one it does not give, or gives malformed, is zero.
func Number(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// Year is the year of a date, or zero for the zero time.
func Year(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return t.Year()
}

// Rated is a certificate a country gives a title, by the country's ISO 3166-1 alpha-2 code.
type Rated struct {
	Country     string
	Certificate string
}

// Certificate is the certificate a title is given in country, else in the US, else the first any
// country gives it, as Jellyfin picks one; one from elsewhere than country is written with its own
// (US:PG-13), so it is read in its own system. "" where none is given.
func Certificate(country string, given []Rated) string {
	given = slices.DeleteFunc(slices.Clone(given), func(r Rated) bool { return r.Certificate == "" })
	for _, from := range []string{country, "US"} {
		if i := slices.IndexFunc(given, func(r Rated) bool { return r.Country == from }); i >= 0 {
			return written(given[i], country)
		}
	}
	if len(given) == 0 {
		return ""
	}
	return written(given[0], country)
}

func written(r Rated, country string) string {
	if r.Country == country {
		return r.Certificate
	}
	return r.Country + ":" + r.Certificate
}

// KeepPictures is how many pictures of each kind a provider's are kept, best first.
const KeepPictures = 10

// Preferred is the best KeepPictures of pictures as Jellyfin ranks them: those in the preferred
// languages, in that order, before the rest, and among each the better first.
func Preferred[T any](pictures []T, language func(T) string, better func(a, b T) int, preferred ...string) []T {
	rank := func(p T) int {
		if i := slices.Index(preferred, language(p)); i >= 0 {
			return i
		}
		return len(preferred)
	}
	pictures = slices.Clone(pictures)
	slices.SortStableFunc(pictures, func(a, b T) int { return cmp.Or(cmp.Compare(rank(a), rank(b)), better(a, b)) })
	return pictures[:min(len(pictures), KeepPictures)]
}
