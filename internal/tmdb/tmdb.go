// Package tmdb asks The Movie Database about films and shows.
package tmdb

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

const (
	baseURL = "https://api.themoviedb.org/3"
	// imageURL serves a picture at the size it was uploaded; the server sizes it for clients.
	imageURL = "https://image.tmdb.org/t/p/original"
	// keepPictures is how many of each kind are kept, best first.
	keepPictures = 10
)

// DefaultToken is the project's own API read access token, shipped in the source as Jellyfin
// ships its key, so matching works without an account. An operator's own token replaces it.
const DefaultToken = "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJkYzYwYTNmZmYyZTRlOWQyZmU3ZTliYzgzYWI1ODNhYSIsIm5iZiI6MTc2MTg2OTQzNi41NDEsInN1YiI6IjY5MDNmZTdjMDIyZTUxOWZlMTJmZGI1YyIsInNjb3BlcyI6WyJhcGlfcmVhZCJdLCJ2ZXJzaW9uIjoxfQ.H6Hra5soywDrjrzesbmH2wzHscr1Vx5ZwfhPTa5nN1Y" //nolint:gosec // public by design

// limit keeps every node together well under TMDB's rate limit of about 50 requests a second.
var limit = kv.Limit{Every: 50 * time.Millisecond, Burst: 20}

var ErrNotFound = errors.New("tmdb: not found")

type Kind string

const (
	Movie Kind = "movie"
	Show  Kind = "tv"
)

type limiter interface {
	Allow(ctx context.Context, key string, l kv.Limit) (time.Duration, error)
}

type Client struct {
	base          string
	token         string
	language      string
	videoLanguage string
	country       string
	http          *http.Client
	limits        limiter
}

// New makes a client that authenticates with an API read access token and asks for metadata in
// language, an IETF tag such as en-US whose region picks the certificates.
func New(token, language string, limits limiter) *Client {
	videoLanguage, country, _ := strings.Cut(language, "-")
	return &Client{
		base: baseURL, token: token, language: language, videoLanguage: videoLanguage, country: strings.ToUpper(country),
		http: &http.Client{Timeout: 30 * time.Second}, limits: limits,
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, into any) error {
	if err := kv.Wait(ctx, c.limits, "tmdb", limit); err != nil {
		return err
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("language", c.language)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(into)
	case http.StatusNotFound:
		return ErrNotFound
	}
	return fmt.Errorf("tmdb %s: %s", path, resp.Status)
}

type result struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
}

func (r result) match() domain.Candidate {
	return domain.Candidate{
		ID: r.ID, Title: cmp.Or(r.Title, r.Name), OriginalTitle: cmp.Or(r.OriginalTitle, r.OriginalName),
		Year: year(date(cmp.Or(r.ReleaseDate, r.FirstAirDate))),
	}
}

func matches(rs []result) []domain.Candidate {
	out := make([]domain.Candidate, len(rs))
	for i, r := range rs {
		out[i] = r.match()
	}
	return out
}

// Search answers TMDB's ranking of titles named title, released in year where it is not zero.
func (c *Client) Search(ctx context.Context, kind Kind, title string, year int) ([]domain.Candidate, error) {
	q := url.Values{"query": {title}, "include_adult": {"false"}}
	if year != 0 {
		q.Set(map[Kind]string{Movie: "year", Show: "first_air_date_year"}[kind], strconv.Itoa(year))
	}
	var page struct {
		Results []result `json:"results"`
	}
	if err := c.get(ctx, "/search/"+string(kind), q, &page); err != nil {
		return nil, err
	}
	return matches(page.Results), nil
}

// Find answers the titles of kind that another provider's id names.
func (c *Client) Find(ctx context.Context, kind Kind, provider domain.Provider, id string) ([]domain.Candidate, error) {
	source := map[domain.Provider]string{domain.ProviderIMDb: "imdb_id", domain.ProviderTVDB: "tvdb_id"}[provider]
	if source == "" {
		return nil, fmt.Errorf("tmdb: cannot find by %s id", provider)
	}
	var found struct {
		Movies []result `json:"movie_results"`
		Shows  []result `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, &found); err != nil {
		return nil, err
	}
	if kind == Movie {
		return matches(found.Movies), nil
	}
	return matches(found.Shows), nil
}

type named struct {
	Name string `json:"name"`
}

type details struct {
	BelongsToCollection *struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		PosterPath   string `json:"poster_path"`
		BackdropPath string `json:"backdrop_path"`
	} `json:"belongs_to_collection"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
	result
	Overview            string  `json:"overview"`
	Tagline             string  `json:"tagline"`
	Genres              []named `json:"genres"`
	ProductionCompanies []named `json:"production_companies"`
	Networks            []named `json:"networks"`
	ExternalIDs         struct {
		IMDb string `json:"imdb_id"`
		TVDB int    `json:"tvdb_id"`
	} `json:"external_ids"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	Videos struct {
		Results []video `json:"results"`
	} `json:"videos"`
	Images struct {
		Posters   []image `json:"posters"`
		Backdrops []image `json:"backdrops"`
		Logos     []image `json:"logos"`
	} `json:"images"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

// Details answers what TMDB says about a title, with its certificate in the client's country.
func (c *Client) Details(ctx context.Context, kind Kind, id int) (domain.Metadata, error) {
	extra := map[Kind]string{Movie: "release_dates,external_ids,videos,images", Show: "content_ratings,external_ids,videos,images"}[kind]
	q := url.Values{
		"append_to_response": {extra},
		// Videos and pictures in the metadata language, and those in none: most trailers' music,
		// and backdrops and posters without lettering.
		"include_video_language": {c.videoLanguage + ",null"},
		"include_image_language": {c.videoLanguage + ",null"},
	}
	var d details
	if err := c.get(ctx, fmt.Sprintf("/%s/%d", kind, id), q, &d); err != nil {
		return domain.Metadata{}, err
	}
	m := d.match()
	released := date(cmp.Or(d.ReleaseDate, d.FirstAirDate))
	out := domain.Metadata{
		Title: m.Title, OriginalTitle: m.OriginalTitle, Overview: d.Overview, Tagline: d.Tagline,
		ReleaseDate: released, Year: year(released),
		Genres: names(d.Genres), Studios: names(append(d.ProductionCompanies, d.Networks...)),
		IDs: map[domain.Provider]string{domain.ProviderTMDB: strconv.Itoa(id)},
	}
	if b := d.BelongsToCollection; b != nil {
		out.Collections = []domain.Grouping{{
			ID: strconv.Itoa(b.ID), Title: b.Name,
			Artwork: slices.Concat(picture(domain.ArtworkPoster, b.PosterPath), picture(domain.ArtworkBackdrop, b.BackdropPath)),
		}}
	}
	if d.VoteCount > 0 {
		out.Ratings = []domain.Rating{{Site: domain.SiteTMDB, Score: d.VoteAverage * 10, Votes: d.VoteCount}}
	}
	if d.ExternalIDs.IMDb != "" {
		out.IDs[domain.ProviderIMDb] = d.ExternalIDs.IMDb
	}
	if d.ExternalIDs.TVDB != 0 {
		out.IDs[domain.ProviderTVDB] = strconv.Itoa(d.ExternalIDs.TVDB)
	}
	for _, r := range d.ReleaseDates.Results {
		for _, rd := range r.Dates {
			if r.Country == c.country {
				out.Certificate = cmp.Or(out.Certificate, rd.Certification)
			}
		}
	}
	for _, r := range d.ContentRatings.Results {
		if r.Country == c.country {
			out.Certificate = cmp.Or(out.Certificate, r.Rating)
		}
	}
	out.Artwork = slices.Concat(
		pictures(domain.ArtworkPoster, d.Images.Posters, c.videoLanguage),
		pictures(domain.ArtworkBackdrop, d.Images.Backdrops, ""),
		pictures(domain.ArtworkLogo, d.Images.Logos, c.videoLanguage),
	)
	videos := d.Videos.Results
	// The studio's own first, then the newest.
	slices.SortStableFunc(videos, func(a, b video) int {
		if a.Official != b.Official {
			return map[bool]int{true: -1, false: 1}[a.Official]
		}
		return b.Published.Compare(a.Published)
	})
	for _, v := range videos {
		out.Videos = append(out.Videos, domain.RemoteVideo{
			Kind: cmp.Or(videoKinds[v.Type], domain.ExtraOther), Site: v.Site, Key: v.Key, Name: v.Name,
			Language: v.Language, Published: v.Published,
		})
	}
	return out, nil
}

type image struct {
	Path     string  `json:"file_path"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Language string  `json:"iso_639_1"`
	Votes    float64 `json:"vote_average"`
}

// pictures orders a kind's pictures, the preferred language first (a poster's lettering in the
// reader's language, a backdrop with none), then by TMDB's votes, and keeps the best.
func pictures(kind domain.ArtworkKind, images []image, preferred string) []domain.Artwork {
	images = slices.Clone(images)
	slices.SortStableFunc(images, func(a, b image) int {
		if (a.Language == preferred) != (b.Language == preferred) {
			return map[bool]int{true: -1, false: 1}[a.Language == preferred]
		}
		return cmp.Compare(b.Votes, a.Votes)
	})
	out := make([]domain.Artwork, 0, min(len(images), keepPictures))
	for _, im := range images[:min(len(images), keepPictures)] {
		out = append(out, domain.Artwork{
			Kind: kind, URL: imageURL + im.Path, Language: im.Language, Width: im.Width, Height: im.Height,
		})
	}
	return out
}

func picture(kind domain.ArtworkKind, path string) []domain.Artwork {
	if path == "" {
		return nil
	}
	return []domain.Artwork{{Kind: kind, URL: imageURL + path}}
}

type video struct {
	Type      string    `json:"type"`
	Site      string    `json:"site"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Language  string    `json:"iso_639_1"`
	Official  bool      `json:"official"`
	Published time.Time `json:"published_at"`
}

// videoKinds maps TMDB's video types to extra kinds; Opening Credits and any type TMDB adds are
// other.
var videoKinds = map[string]domain.ExtraKind{
	"Trailer": domain.ExtraTrailer, "Teaser": domain.ExtraTeaser, "Clip": domain.ExtraClip,
	"Featurette": domain.ExtraFeaturette, "Behind the Scenes": domain.ExtraBehindTheScenes,
	"Bloopers": domain.ExtraBlooper,
}

func (c *Client) Season(ctx context.Context, show, number int) (domain.SeasonMetadata, error) {
	var s struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
		AirDate  string `json:"air_date"`
		Poster   string `json:"poster_path"`
		Episodes []struct {
			Number   int    `json:"episode_number"`
			Name     string `json:"name"`
			Overview string `json:"overview"`
			AirDate  string `json:"air_date"`
			Still    string `json:"still_path"`
		} `json:"episodes"`
	}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", show, number), nil, &s); err != nil {
		return domain.SeasonMetadata{}, err
	}
	aired := date(s.AirDate)
	out := domain.SeasonMetadata{
		Metadata: domain.Metadata{
			Title: s.Name, Overview: s.Overview, ReleaseDate: aired, Year: year(aired),
			Artwork: picture(domain.ArtworkPoster, s.Poster),
		},
		Episodes: make(map[int]domain.Metadata, len(s.Episodes)),
	}
	for _, e := range s.Episodes {
		aired := date(e.AirDate)
		out.Episodes[e.Number] = domain.Metadata{
			Title: e.Name, Overview: e.Overview, ReleaseDate: aired, Year: year(aired),
			Artwork: picture(domain.ArtworkThumb, e.Still),
		}
	}
	return out, nil
}

// names lists each name once: a network is often one of its show's production companies too.
func names(ns []named) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		if !slices.Contains(out, n.Name) {
			out = append(out, n.Name)
		}
	}
	return out
}

// date reads TMDB's dates; an absent one is the zero time.
func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func year(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return t.Year()
}
