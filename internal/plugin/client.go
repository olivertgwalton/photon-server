package plugin

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// client is a registered plugin as a provider. It implements every capability, and answers the
// ones its manifest names at the version the server speaks.
type client struct {
	manifest pluginv1.Manifest
	speaks   map[domain.Capability]int
	base     string
	http     *http.Client
	settings provider.Settings
}

func (c *client) source() domain.FieldSource {
	return domain.PluginSource(c.manifest.ID)
}

func (c *client) Info() provider.Info {
	info := provider.Info{ID: c.source(), Name: c.manifest.Name}
	for _, k := range c.manifest.Kinds {
		info.Kinds = append(info.Kinds, domain.ItemKind(k))
	}
	for _, s := range c.manifest.Settings {
		info.Settings = append(info.Settings, provider.Setting{Key: s.Key, Name: s.Name, Secret: s.Secret, Required: s.Required})
	}
	return info
}

func (c *client) Answers(capability domain.Capability) bool {
	_, ok := c.speaks[capability]
	return ok
}

// spoken is the version of each capability in m that the server speaks.
func spoken(m pluginv1.Manifest) map[domain.Capability]int {
	out := map[domain.Capability]int{}
	for _, c := range m.Capabilities {
		if v, ok := pluginv1.Speaks[c.Name]; ok && c.Version == v {
			out[domain.Capability(c.Name)] = v
		}
	}
	return out
}

// hearing is the events m hears and where the plugin at base is told of them.
func hearing(m pluginv1.Manifest, base string) store.Hearing {
	h := store.Hearing{URL: base + fmt.Sprintf("/%s/v%d/event", domain.CapabilityEvents, pluginv1.Speaks[string(domain.CapabilityEvents)])}
	for _, c := range m.Capabilities {
		if c.Name != string(domain.CapabilityEvents) || c.Version != pluginv1.Speaks[c.Name] {
			continue
		}
		for _, e := range c.Events {
			if k := domain.EventKind(e); k.Hookable() && !slices.Contains(h.Kinds, k) {
				h.Kinds = append(h.Kinds, k)
			}
		}
	}
	return h
}

// post makes one of a capability's calls, verb, with the settings an admin set for the plugin in
// the body, or answers provider.ErrNotConfigured where one it requires is not set.
func post[Out, In any](ctx context.Context, c *client, capability domain.Capability, verb string, body func(pluginv1.Settings) In) (Out, error) {
	var out Out
	set, err := c.settings(ctx)
	if err != nil {
		return out, err
	}
	sent := pluginv1.Settings{}
	var secrets []string
	for _, s := range c.manifest.Settings {
		v := set[s.Key]
		if v == "" {
			if s.Required {
				return out, provider.ErrNotConfigured
			}
			continue
		}
		sent[s.Key] = v
		if s.Secret {
			secrets = append(secrets, v)
		}
	}
	in := body(sent)
	path := fmt.Sprintf("/%s/v%d/%s", capability, c.speaks[capability], verb)
	return call[In, Out](ctx, c.http, "plugin "+c.manifest.ID, http.MethodPost, c.base, path, &in, secrets)
}

func sentLocale(loc domain.Locale) pluginv1.Locale {
	return pluginv1.Locale{Language: loc.Language, Country: loc.Country, Artwork: string(loc.Artwork)}
}

func (c *client) Match(ctx context.Context, loc domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	out, err := post[pluginv1.MatchResponse](ctx, c, domain.CapabilityDescribe, "match", func(s pluginv1.Settings) pluginv1.MatchRequest {
		return pluginv1.MatchRequest{Settings: s, Locale: sentLocale(loc), Kind: string(kind), Title: h.Title, Year: h.Year, IDs: sentIDs(h.IDs)}
	})
	return out.ID, err
}

func (c *client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	out, err := post[pluginv1.DescribeResponse](ctx, c, domain.CapabilityDescribe, "describe", func(s pluginv1.Settings) pluginv1.DescribeRequest {
		return pluginv1.DescribeRequest{Settings: s, Locale: sentLocale(loc), Kind: string(kind), ID: id, Seasons: seasons.Numbers, Order: string(seasons.Order), SeasonScope: string(seasons.Scope)}
	})
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	said := map[int]domain.SeasonMetadata{}
	for _, s := range out.Seasons {
		season := domain.SeasonMetadata{Metadata: c.metadata(s.Metadata), Episodes: map[int]domain.Metadata{}}
		for _, e := range s.Episodes {
			season.Episodes[e.Number] = c.metadata(e.Metadata)
		}
		said[s.Number] = season
	}
	return c.metadata(out.Metadata), said, nil
}

func (c *client) Candidates(ctx context.Context, loc domain.Locale, kind domain.ItemKind, title string, year int) ([]domain.Candidate, error) {
	out, err := post[pluginv1.SearchResponse](ctx, c, domain.CapabilitySearch, "search", func(s pluginv1.Settings) pluginv1.SearchRequest {
		return pluginv1.SearchRequest{Settings: s, Locale: sentLocale(loc), Kind: string(kind), Title: title, Year: year}
	})
	found := make([]domain.Candidate, 0, len(out.Results))
	for _, r := range out.Results {
		if r.ID != "" {
			found = append(found, domain.Candidate{ID: r.ID, Title: r.Title, OriginalTitle: r.OriginalTitle, Year: r.Year, Overview: r.Overview, Poster: web(r.Poster)})
		}
	}
	return found, err
}

func (c *client) Ratings(ctx context.Context, kind domain.ItemKind, ids map[domain.Provider]string) ([]domain.Rating, error) {
	out, err := post[pluginv1.RatingsResponse](ctx, c, domain.CapabilityRate, "ratings", func(s pluginv1.Settings) pluginv1.RatingsRequest {
		return pluginv1.RatingsRequest{Settings: s, Kind: string(kind), IDs: sentIDs(ids)}
	})
	var ratings []domain.Rating
	for _, r := range out.Ratings {
		if site, err := domain.Parse("rating site", r.Site, domain.RatingSites()); err == nil {
			ratings = append(ratings, domain.Rating{Site: site, Score: min(max(r.Score, 0), 100), Votes: max(r.Votes, 0)})
		}
	}
	return ratings, err
}

func (c *client) DescribePerson(ctx context.Context, loc domain.Locale, ids map[domain.Provider]string) (domain.Person, error) {
	out, err := post[pluginv1.PersonResponse](ctx, c, domain.CapabilityPerson, "person", func(s pluginv1.Settings) pluginv1.PersonRequest {
		return pluginv1.PersonRequest{Settings: s, Locale: sentLocale(loc), IDs: sentIDs(ids)}
	})
	return domain.Person{
		Name: out.Name, Biography: out.Biography, Born: provider.Date(out.Born), Died: provider.Date(out.Died),
		Birthplace: out.Birthplace, Photo: web(out.Photo),
	}, err
}

func (c *client) List(ctx context.Context, id string) ([]domain.Listed, error) {
	out, err := post[pluginv1.ListResponse](ctx, c, domain.CapabilityList, "list", func(s pluginv1.Settings) pluginv1.ListRequest {
		return pluginv1.ListRequest{Settings: s, ID: id}
	})
	var listed []domain.Listed
	for _, t := range out.Titles {
		kind := domain.ItemKind(t.Kind)
		if ids := c.ids(t.IDs); len(ids) > 0 && slices.Contains(c.Info().Kinds, kind) {
			listed = append(listed, domain.Listed{Kind: kind, IDs: ids, Title: t.Title, Year: t.Year})
		}
	}
	return listed, err
}

func (c *client) Streams(ctx context.Context, t domain.Streamed) ([]domain.Offer, error) {
	out, err := post[pluginv1.StreamsResponse](ctx, c, domain.CapabilityStream, "streams", func(s pluginv1.Settings) pluginv1.StreamsRequest {
		return pluginv1.StreamsRequest{Settings: s, Kind: string(t.Kind), IDs: sentIDs(t.IDs), Season: t.Season, Episode: t.Episode}
	})
	var offers []domain.Offer
	for _, s := range out.Streams {
		if u, err := url.Parse(web(s.URL)); err == nil && u.Host != "" && s.Key != "" {
			offers = append(offers, domain.Offer{Key: s.Key, Name: cmp.Or(s.Name, s.Filename), Filename: s.Filename, Size: max(s.Size, 0), URL: u, From: domain.OfferedFrom(c.base)})
		}
	}
	return offers, err
}

func (c *client) SearchSubtitles(ctx context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	out, err := post[pluginv1.SubtitlesResponse](ctx, c, domain.CapabilitySubtitles, "search", func(s pluginv1.Settings) pluginv1.SubtitlesRequest {
		return pluginv1.SubtitlesRequest{Settings: s, Kind: string(q.Kind), IDs: sentIDs(q.IDs), Season: q.Season, Episode: q.Episode, Hash: q.Hash, Language: q.Language.String()}
	})
	var found []domain.FoundSubtitle
	for _, f := range out.Subtitles {
		if lang, err := language.Parse(f.Language); err == nil && f.ID != "" {
			found = append(found, domain.FoundSubtitle{
				Source: c.source(), ID: f.ID, Language: lang, Release: f.Release, HearingImpaired: f.HearingImpaired,
				Forced: f.Forced, ForRelease: f.ForRelease && q.Hash != "", Downloads: max(f.Downloads, 0),
			})
		}
	}
	return found, err
}

func (c *client) FetchSubtitle(ctx context.Context, id string) ([]byte, error) {
	out, err := post[pluginv1.FetchResponse](ctx, c, domain.CapabilitySubtitles, "fetch", func(s pluginv1.Settings) pluginv1.FetchRequest {
		return pluginv1.FetchRequest{Settings: s, ID: id}
	})
	if err == nil && out.SubRip == "" {
		return nil, fmt.Errorf("plugin %s: subtitle %s: %w", c.manifest.ID, id, provider.ErrNotFound)
	}
	return []byte(out.SubRip), err
}

func (c *client) Segments(ctx context.Context, q domain.SegmentQuery) ([]domain.Marker, error) {
	length := q.Duration.Milliseconds()
	out, err := post[pluginv1.MarkersResponse](ctx, c, domain.CapabilitySegments, "markers", func(s pluginv1.Settings) pluginv1.MarkersRequest {
		return pluginv1.MarkersRequest{Settings: s, Kind: string(q.Kind), IDs: sentIDs(q.IDs), Season: q.Season, Episode: q.Episode, DurationMS: length}
	})
	var markers []domain.Marker
	for _, m := range out.Markers {
		kind := domain.MarkerKind(m.Kind)
		taken := slices.ContainsFunc(markers, func(t domain.Marker) bool { return t.Kind == kind })
		if slices.Contains(domain.MarkerKinds(), kind) && !taken && 0 <= m.StartMS && m.StartMS < m.EndMS && m.EndMS <= length {
			markers = append(markers, domain.Marker{Kind: kind, StartMS: m.StartMS, EndMS: m.EndMS})
		}
	}
	return markers, err
}

// metadata is what the plugin said in the server's terms. Anything the server has no name for (a
// kind of picture, a site, a credit) or will not fetch (a picture not on the web) is left out.
func (c *client) metadata(m pluginv1.Metadata) domain.Metadata {
	out := domain.Metadata{
		Title: m.Title, SortTitle: m.SortTitle, OriginalTitle: m.OriginalTitle, Overview: m.Overview,
		Tagline: m.Tagline, Certificate: m.Certificate, ReleaseDate: provider.Date(m.ReleaseDate), Year: m.Year,
		Genres: m.Genres, Studios: m.Studios, IDs: c.ids(m.IDs),
	}
	for _, a := range m.Artwork {
		if kind := domain.ArtworkKind(a.Kind); slices.Contains(domain.ArtworkKinds(), kind) && web(a.URL) != "" {
			out.Artwork = append(out.Artwork, domain.Artwork{Kind: kind, URL: a.URL, Language: a.Language, Width: a.Width, Height: a.Height})
		}
	}
	for _, v := range m.Videos {
		if kind := domain.ExtraKind(v.Kind); slices.Contains(domain.ExtraKinds(), kind) && v.Site != "" && v.Key != "" {
			out.Videos = append(out.Videos, domain.RemoteVideo{Kind: kind, Site: v.Site, Key: v.Key, Name: v.Name, Language: v.Language, Published: v.Published})
		}
	}
	for _, cr := range m.Credits {
		if kind := domain.CreditKind(cr.Kind); slices.Contains(domain.CreditKinds(), kind) && cr.Name != "" {
			out.Credits = append(out.Credits, domain.Credit{Name: cr.Name, IDs: c.ids(cr.IDs), Photo: web(cr.Photo), Kind: kind, Role: cr.Role})
		}
	}
	return out
}

// ids keeps the ids a title can carry: the built-in providers' and the plugin's own.
func (c *client) ids(in map[string]string) map[domain.Provider]string {
	out := map[domain.Provider]string{}
	for k, v := range in {
		p := domain.Provider(k)
		if v != "" && (slices.Contains(domain.Providers(), p) || p == domain.Provider(c.source())) {
			out[p] = v
		}
	}
	return out
}

func sentIDs(ids map[domain.Provider]string) map[string]string {
	out := make(map[string]string, len(ids))
	for p, v := range ids {
		out[string(p)] = v
	}
	return out
}

// web answers an address the artwork cache can fetch, http or https, or nothing.
func web(address string) string {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return address
}
