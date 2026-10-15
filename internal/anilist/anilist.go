// Package anilist asks AniList about anime. An AniList entry is one cour, film or OVA, so a show is
// described by the entry it opens with, and each of its seasons by the entry the season opens
// with, found through the anime lists.
package anilist

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// limit keeps every node together under AniList's 30 requests a minute.
var limit = kv.Limit{Every: 2 * time.Second, Burst: 3}

type Client struct {
	api   provider.Client
	lists animelist.Lists
}

func New(l animelist.Lists, limits kv.Limiter) *Client {
	return &Client{api: provider.Client{Name: "anilist", Base: "https://graphql.anilist.co", Limits: limits, Limit: limit}, lists: l}
}

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceAniList, Name: "AniList", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow}}
}

// Match finds a title on AniList by its AniList id, else by the entry the anime lists find it
// opens with. A title the lists do not name is not anime, and is not searched for.
func (c *Client) Match(ctx context.Context, _ domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	if id := h.IDs[domain.ProviderAniList]; id != "" {
		return id, nil
	}
	e, ok, err := animelist.Find(ctx, c.lists, kind, h.IDs)
	if err != nil || !ok || e.AniList == 0 {
		return "", err
	}
	return strconv.Itoa(e.AniList), nil
}

// Describe answers what AniList says of a title, by the entry matched, and of each season of a show
// asked for by the entry the anime lists say it opens with, as its episodes are numbered. A show
// numbered in an order the lists do not place entries in is described without its seasons.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	opening, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, provider.ErrNotFound
	}
	entry, _, err := c.lists.AnimeEntry(ctx, domain.ProviderAniList, id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	var bySeason map[int]int
	if kind == domain.ItemShow {
		if bySeason, err = c.seasons(ctx, entry, seasons); err != nil {
			return domain.Metadata{}, nil, err
		}
	}
	ids := []int{opening}
	for _, e := range bySeason {
		ids = append(ids, e)
	}
	media, err := c.media(ctx, ids)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	show, ok := media[opening]
	if !ok {
		return domain.Metadata{}, nil, provider.ErrNotFound
	}
	m := show.metadata(loc, true)
	m.IDs = map[domain.Provider]string{domain.ProviderAniList: id}
	if entry.AniDB != 0 {
		m.IDs[domain.ProviderAniDB] = strconv.Itoa(entry.AniDB)
	}
	said := map[int]domain.SeasonMetadata{}
	for n, e := range bySeason {
		if s, ok := media[e]; ok {
			said[n] = domain.SeasonMetadata{Metadata: s.metadata(loc, false)}
		}
	}
	return m, said, nil
}

// seasons answers the AniList entry each season asked for opens with, as the show's episodes are
// numbered: in its seasons as aired, or as one absolute run.
func (c *Client) seasons(ctx context.Context, entry animelist.Entry, asked domain.SeasonRequest) (map[int]int, error) {
	p := asked.Numbering
	if p == "" || entry.Placing(p).Show == 0 {
		return nil, nil
	}
	var in func(season int) int
	switch asked.Order {
	case domain.OrderAired:
		in = func(season int) int { return season }
	case domain.OrderAbsolute:
		in = func(int) int { return animelist.Absolute }
	case domain.OrderDVD, domain.OrderGroup:
		return nil, nil
	}
	entries, err := c.lists.AnimeShow(ctx, p, entry.Placing(p).Show)
	if err != nil {
		return nil, err
	}
	numbers := asked.Numbers
	if asked.Scope == domain.SeasonsEvery {
		numbers = nil
		for _, e := range entries {
			if s := e.Placing(p).Season; s > 0 {
				numbers = append(numbers, s)
			}
		}
	}
	out := map[int]int{}
	for _, n := range numbers {
		// The specials are many entries' OVAs and films, which no one entry describes.
		if n == 0 {
			continue
		}
		if e, ok := animelist.First(entries, p, in(n)); ok && e.AniList != 0 {
			out[n] = e.AniList
		}
	}
	return out, nil
}

const mediaQuery = `query($ids: [Int]) { Page(perPage: 50) { media(id_in: $ids, type: ANIME) {
	id title { romaji english native } description(asHtml: false) startDate { year month day }
	genres averageScore coverImage { extraLarge } bannerImage
	studios(isMain: true) { nodes { name isAnimationStudio } }
	characters(sort: [ROLE, RELEVANCE], perPage: 25) { edges { node { name { full } }
		voiceActors(language: JAPANESE, sort: [RELEVANCE]) { id name { full } image { large } } } }
	staff(sort: [RELEVANCE], perPage: 25) { edges { role node { id name { full } image { large } } } } } } }`

type media struct {
	ID    int `json:"id"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	Description string `json:"description"`
	StartDate   struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"startDate"`
	Genres       []string `json:"genres"`
	AverageScore int      `json:"averageScore"`
	CoverImage   struct {
		ExtraLarge string `json:"extraLarge"`
	} `json:"coverImage"`
	BannerImage string `json:"bannerImage"`
	Studios     struct {
		Nodes []struct {
			Name      string `json:"name"`
			Animation bool   `json:"isAnimationStudio"`
		} `json:"nodes"`
	} `json:"studios"`
	Characters struct {
		Edges []struct {
			Node struct {
				Name name `json:"name"`
			} `json:"node"`
			VoiceActors []staff `json:"voiceActors"`
		} `json:"edges"`
	} `json:"characters"`
	Staff struct {
		Edges []struct {
			Role string `json:"role"`
			Node staff  `json:"node"`
		} `json:"edges"`
	} `json:"staff"`
}

type name struct {
	Full string `json:"full"`
}

type staff struct {
	ID    int  `json:"id"`
	Name  name `json:"name"`
	Image struct {
		Large string `json:"large"`
	} `json:"image"`
}

// media answers AniList's entries by their ids, in one request.
func (c *Client) media(ctx context.Context, ids []int) (map[int]media, error) {
	var body struct {
		Data struct {
			Page struct {
				Media []media `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	err := c.api.Do(ctx, provider.Request{
		Method: http.MethodPost, Path: "/", Header: http.Header{"Accept": {"application/json"}},
		Body: map[string]any{"query": mediaQuery, "variables": map[string]any{"ids": ids}},
	}, &body)
	if err != nil {
		return nil, err
	}
	out := make(map[int]media, len(body.Data.Page.Media))
	for _, m := range body.Data.Page.Media {
		out[m.ID] = m
	}
	return out, nil
}

// roles are the staff credits kept, by the role AniList names, and what they are credited as.
var roles = map[string]domain.CreditKind{
	"Director": domain.CreditDirector, "Original Creator": domain.CreditCreator, "Original Story": domain.CreditCreator,
	"Series Composition": domain.CreditWriter, "Music": domain.CreditComposer,
}

var (
	tags = regexp.MustCompile(`<[^>]*>`)
	// gaps are the blank lines a description's breaks and newlines leave together, a paragraph's.
	gaps = regexp.MustCompile(`\n\s*\n\s*`)
)

// metadata is what an entry says of a title, or of a season, which takes no credits of its own.
// Its title is the one in the language asked in, as AniList names it: in Japanese its own, else
// in English where it has one, else romanised.
func (m media) metadata(loc domain.Locale, credited bool) domain.Metadata {
	title := cmp.Or(m.Title.English, m.Title.Romaji)
	if strings.HasPrefix(loc.Language, "ja") {
		title = cmp.Or(m.Title.Native, title)
	}
	out := domain.Metadata{
		Title: title, OriginalTitle: m.Title.Native, Genres: m.Genres,
		Overview: strings.TrimSpace(gaps.ReplaceAllString(tags.ReplaceAllString(strings.ReplaceAll(m.Description, "<br>", "\n"), ""), "\n\n")),
	}
	if d := m.StartDate; d.Year != 0 {
		out.Year = d.Year
		if d.Month != 0 && d.Day != 0 {
			out.ReleaseDate = time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
		}
	}
	for _, s := range m.Studios.Nodes {
		if s.Animation {
			out.Studios = append(out.Studios, s.Name)
		}
	}
	if m.CoverImage.ExtraLarge != "" {
		out.Artwork = append(out.Artwork, domain.Artwork{Kind: domain.ArtworkPoster, URL: m.CoverImage.ExtraLarge})
	}
	if m.BannerImage != "" {
		out.Artwork = append(out.Artwork, domain.Artwork{Kind: domain.ArtworkBanner, URL: m.BannerImage})
	}
	if m.AverageScore > 0 {
		out.Ratings = []domain.Rating{{Site: domain.SiteAniList, Score: float64(m.AverageScore)}}
	}
	if !credited {
		return out
	}
	for _, c := range m.Characters.Edges {
		for _, va := range c.VoiceActors {
			out.Credits = append(out.Credits, va.credit(domain.CreditActor, c.Node.Name.Full))
		}
	}
	for _, s := range m.Staff.Edges {
		// A role may say which episodes: "Director (eps 1-12)".
		role, _, _ := strings.Cut(s.Role, " (")
		if kind, ok := roles[role]; ok {
			out.Credits = append(out.Credits, s.Node.credit(kind, s.Role))
		}
	}
	return out
}

func (s staff) credit(kind domain.CreditKind, role string) domain.Credit {
	c := domain.Credit{Name: s.Name.Full, IDs: map[domain.Provider]string{domain.ProviderAniList: strconv.Itoa(s.ID)}, Kind: kind, Role: role}
	// AniList pictures someone without a picture as a silhouette, which is no picture of them.
	if !strings.HasSuffix(s.Image.Large, "/default.jpg") {
		c.Photo = s.Image.Large
	}
	return c
}
