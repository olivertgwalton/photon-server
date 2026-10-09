package plugin

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// client is a registered plugin as a provider. It implements every capability, and answers the
// ones its manifest names.
type client struct {
	manifest pluginv1.Manifest
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
	return slices.Contains(c.manifest.Capabilities, string(capability))
}

// post asks the plugin at path, with the settings an admin set for it in the body, or answers
// provider.ErrNotConfigured where one it requires is not set.
func post[Out, In any](ctx context.Context, c *client, path string, body func(pluginv1.Settings) In) (Out, error) {
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
	return call[In, Out](ctx, c.http, "plugin "+c.manifest.ID, http.MethodPost, c.base, path, &in, secrets)
}

func sentLocale(loc domain.Locale) pluginv1.Locale {
	return pluginv1.Locale{Language: loc.Language, Country: loc.Country, Artwork: string(loc.Artwork)}
}

func (c *client) Match(ctx context.Context, loc domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	out, err := post[pluginv1.MatchResponse](ctx, c, "/match", func(s pluginv1.Settings) pluginv1.MatchRequest {
		return pluginv1.MatchRequest{Settings: s, Locale: sentLocale(loc), Kind: string(kind), Title: h.Title, Year: h.Year, IDs: sentIDs(h.IDs)}
	})
	return out.ID, err
}

func (c *client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	out, err := post[pluginv1.DescribeResponse](ctx, c, "/describe", func(s pluginv1.Settings) pluginv1.DescribeRequest {
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
	out, err := post[pluginv1.SearchResponse](ctx, c, "/search", func(s pluginv1.Settings) pluginv1.SearchRequest {
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
	out, err := post[pluginv1.RatingsResponse](ctx, c, "/ratings", func(s pluginv1.Settings) pluginv1.RatingsRequest {
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
	out, err := post[pluginv1.PersonResponse](ctx, c, "/person", func(s pluginv1.Settings) pluginv1.PersonRequest {
		return pluginv1.PersonRequest{Settings: s, Locale: sentLocale(loc), IDs: sentIDs(ids)}
	})
	return domain.Person{
		Name: out.Name, Biography: out.Biography, Born: provider.Date(out.Born), Died: provider.Date(out.Died),
		Birthplace: out.Birthplace, Photo: web(out.Photo),
	}, err
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
