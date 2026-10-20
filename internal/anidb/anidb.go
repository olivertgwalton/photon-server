// Package anidb asks AniDB about anime, through its HTTP API. AniDB files each cour, film and OVA
// as an entry of its own, which the anime lists place in a show's seasons; its entries' episodes
// are what it says of a show's. It answers only a client an admin registered with it, at most one
// request every two seconds and none for the same anime twice a day, and bans a client that asks
// more.
package anidb

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	clientSetting  = "client"
	versionSetting = "version"
	// kept is how long an anime's answer is read again rather than asked for, as AniDB asks.
	kept = 24 * time.Hour
	// banned is how long a ban parks AniDB: it does not say how long a ban lasts, from 15 minutes
	// to a day, and asking while banned lengthens it.
	banned = 24 * time.Hour
	// banKey is the answer kept while AniDB has banned the server.
	banKey   = "banned"
	imageURL = "https://cdn.anidb.net/images/main/"
)

// limit keeps every node together to one request every two seconds and a half, over AniDB's two.
var limit = kv.Limit{Every: 2500 * time.Millisecond, Burst: 1}

// answers are AniDB's answers kept, so no anime is asked for twice a day, and the server's ban.
type answers interface {
	kv.Limiter
	KeepAniDB(ctx context.Context, name string, answer []byte, ttl time.Duration) error
	AniDB(ctx context.Context, name string) ([]byte, bool, error)
}

type Client struct {
	settings provider.Settings
	api      provider.Client
	answers  answers
	lists    animelist.Lists
}

func New(settings provider.Settings, l animelist.Lists, a answers) *Client {
	return &Client{
		settings: settings, lists: l, answers: a,
		api: provider.Client{Name: "anidb", Base: "http://api.anidb.net:9001", Limits: a, Limit: limit},
	}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceAniDB, Name: "AniDB", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{
			{Key: clientSetting, Name: "Client name, as registered with AniDB", Required: true},
			{Key: versionSetting, Name: "Client version, as registered with AniDB", Required: true},
		},
	}
}

// Match finds a title on AniDB by its AniDB id, else by the entry the anime lists find it opens
// with. A title the lists do not name is not anime, and is not searched for.
func (c *Client) Match(ctx context.Context, _ domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	if id := h.IDs[domain.ProviderAniDB]; id != "" {
		return id, nil
	}
	e, ok, err := animelist.Find(ctx, c.lists, kind, h.IDs)
	if err != nil || !ok {
		return "", err
	}
	return strconv.Itoa(e.AniDB), nil
}

// Describe answers what AniDB says of a title, by the entry matched, and of each season of a show
// asked for by its entries' episodes, placed in it by the anime lists as its episodes are
// numbered: each season named as the entry it opens with. A show numbered in an order the lists do
// not place entries in is described without its seasons.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	aid, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, provider.ErrNotFound
	}
	opening, err := c.anime(ctx, aid)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	m := opening.metadata(loc)
	m.IDs = map[domain.Provider]string{domain.ProviderAniDB: id}
	entry, _, err := c.lists.AnimeEntry(ctx, domain.ProviderAniDB, id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	if entry.AniList != 0 {
		m.IDs[domain.ProviderAniList] = strconv.Itoa(entry.AniList)
	}
	m.Credits = opening.credits()
	said := map[int]domain.SeasonMetadata{}
	if kind == domain.ItemShow {
		if said, err = c.seasons(ctx, loc, entry, seasons); err != nil {
			return domain.Metadata{}, nil, err
		}
	}
	return m, said, nil
}

// seasons answers the seasons asked for, each with every episode of its entries placed in it.
func (c *Client) seasons(ctx context.Context, loc domain.Locale, entry animelist.Entry, asked domain.SeasonRequest) (map[int]domain.SeasonMetadata, error) {
	p, order := asked.Numbering, asked.Order
	said := map[int]domain.SeasonMetadata{}
	if p == "" || entry.Placing(p).Show == 0 || order != domain.OrderAired && order != domain.OrderAbsolute {
		return said, nil
	}
	// A show numbered as one absolute run is in one season, which the lists call Absolute; its
	// specials are in season 0 whichever its order.
	in := func(season int) int {
		if order == domain.OrderAbsolute && season != 0 {
			return animelist.Absolute
		}
		return season
	}
	entries, err := c.lists.AnimeShow(ctx, p, entry.Placing(p).Show)
	if err != nil {
		return nil, err
	}
	numbers := asked.Numbers
	if asked.Scope == domain.SeasonsEvery {
		numbers = nil
		for _, e := range entries {
			numbers = append(numbers, max(e.Placing(p).Season, 0))
		}
	}
	for _, n := range numbers {
		season := domain.SeasonMetadata{Episodes: map[int]domain.Metadata{}}
		if first, ok := animelist.First(entries, p, in(n)); ok && n != 0 {
			a, err := c.anime(ctx, first.AniDB)
			if err != nil {
				return nil, err
			}
			season.Metadata = a.metadata(loc)
		}
		for _, e := range animelist.In(entries, p, in(n)) {
			a, err := c.anime(ctx, e.AniDB)
			if err != nil {
				return nil, err
			}
			for _, ep := range a.Episodes {
				number, special, ok := ep.number()
				if !ok {
					continue
				}
				if at, eps, ok := e.At(p, order, special, number); ok && at == in(n) {
					for _, shown := range eps {
						season.Episodes[shown] = ep.metadata(loc)
					}
				}
			}
		}
		if len(season.Episodes) > 0 || season.Metadata.Title != "" {
			said[n] = season
		}
	}
	return said, nil
}

// anime answers an entry as AniDB says it: as kept, where it was asked for within the day.
func (c *Client) anime(ctx context.Context, aid int) (anime, error) {
	name := "anime:" + strconv.Itoa(aid)
	data, ok, err := c.answers.AniDB(ctx, name)
	if err != nil {
		return anime{}, err
	}
	if !ok {
		if data, err = c.ask(ctx, aid); err != nil {
			return anime{}, err
		}
		if err := c.answers.KeepAniDB(ctx, name, data, kept); err != nil {
			return anime{}, err
		}
	}
	var refused struct {
		XMLName xml.Name `xml:"error"`
		Message string   `xml:",chardata"`
	}
	if xml.Unmarshal(data, &refused) == nil {
		return anime{}, fmt.Errorf("anidb: anime %d: %s: %w", aid, refused.Message, provider.ErrNotFound)
	}
	var a anime
	err = xml.Unmarshal(data, &a)
	return a, err
}

var errBanned = errors.New("anidb: banned")

// ask asks AniDB for an entry, unless it has banned the server. A refusal is kept as an answer
// is, so the rest of a show does not ask again for it.
func (c *Client) ask(ctx context.Context, aid int) ([]byte, error) {
	if _, isBanned, err := c.answers.AniDB(ctx, banKey); err != nil || isBanned {
		return nil, cmp.Or(err, fmt.Errorf("%w: %w", errBanned, provider.ErrUnavailable))
	}
	set, err := c.settings(ctx)
	if err != nil {
		return nil, err
	}
	client, version := set[clientSetting], set[versionSetting]
	if client == "" || version == "" {
		return nil, provider.ErrNotConfigured
	}
	data, err := c.api.Bytes(ctx, provider.Request{Method: http.MethodGet, Path: "/httpapi", Query: url.Values{
		"request": {"anime"}, "client": {client}, "clientver": {version}, "protover": {"1"}, "aid": {strconv.Itoa(aid)},
	}})
	if err != nil {
		return nil, err
	}
	// AniDB gzips every answer, saying so or not.
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		// The cap on what was sent does not bound what it unzips to.
		if data, err = io.ReadAll(io.LimitReader(r, provider.MaxAnswer+1)); err != nil {
			return nil, err
		}
		if len(data) > provider.MaxAnswer {
			return nil, fmt.Errorf("anidb: anime %d: unzips to more than %d bytes", aid, provider.MaxAnswer)
		}
	}
	if bytes.Contains(data, []byte("<error")) && bytes.Contains(data, []byte("banned")) {
		if err := c.answers.KeepAniDB(ctx, banKey, []byte("1"), banned); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", errBanned, provider.ErrUnavailable)
	}
	return data, nil
}

type title struct {
	Language string `xml:"lang,attr"`
	Type     string `xml:"type,attr"`
	Text     string `xml:",chardata"`
}

type anime struct {
	StartDate   string  `xml:"startdate"`
	Titles      []title `xml:"titles>title"`
	Description string  `xml:"description"`
	Rating      struct {
		Score float64 `xml:",chardata"`
		Votes int     `xml:"count,attr"`
	} `xml:"ratings>permanent"`
	Picture  string `xml:"picture"`
	Creators []struct {
		ID   int    `xml:"id,attr"`
		Type string `xml:"type,attr"`
		Name string `xml:",chardata"`
	} `xml:"creators>name"`
	Characters []struct {
		Name   string `xml:"name"`
		Seiyuu []struct {
			ID      int    `xml:"id,attr"`
			Picture string `xml:"picture,attr"`
			Name    string `xml:",chardata"`
		} `xml:"seiyuu"`
	} `xml:"characters>character"`
	Episodes []episode `xml:"episodes>episode"`
}

type episode struct {
	Number struct {
		Type int    `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"epno"`
	AirDate string  `xml:"airdate"`
	Titles  []title `xml:"title"`
	Summary string  `xml:"summary"`
	Rating  struct {
		Score float64 `xml:",chardata"`
		Votes int     `xml:"votes,attr"`
	} `xml:"rating"`
}

// number answers an episode's number among the entry's regular episodes, or among its specials,
// "S1"; false for its credits, trailers, parodies and the rest.
func (e episode) number() (int, bool, bool) {
	switch e.Number.Type {
	case 1:
		n, err := strconv.Atoi(e.Number.Text)
		return n, false, err == nil
	case 2:
		n, err := strconv.Atoi(strings.TrimPrefix(e.Number.Text, "S"))
		return n, true, err == nil
	}
	return 0, false, false
}

// name answers an entry's or episode's title in the language asked in, as AniDB names it:
// officially in it, else its main title, the romanised Japanese.
func name(titles []title, loc domain.Locale) string {
	lang, _, _ := strings.Cut(loc.Language, "-")
	var main string
	for _, t := range titles {
		switch {
		case t.Language == lang && (t.Type == "official" || t.Type == ""):
			return t.Text
		case t.Type == "main":
			main = t.Text
		}
	}
	if main != "" {
		return main
	}
	for _, t := range titles {
		if t.Language == "en" {
			return t.Text
		}
	}
	return ""
}

// links are AniDB's links to its own pages, which say what they link: "http://anidb.net/ch7888
// [Frieren]".
var links = regexp.MustCompile(`https?://anidb\.net/\S+ \[([^\]]*)\]`)

// plain is an AniDB text without its links and its BBCode tags, "[i]Source: …[/i]".
func plain(s string) string {
	s = links.ReplaceAllString(s, "$1")
	for _, tag := range []string{"[i]", "[/i]", "[b]", "[/b]"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return strings.TrimSpace(s)
}

func (a anime) metadata(loc domain.Locale) domain.Metadata {
	m := domain.Metadata{Title: name(a.Titles, loc), Overview: plain(a.Description)}
	for _, t := range a.Titles {
		if t.Language == "ja" && t.Type == "official" {
			m.OriginalTitle = t.Text
		}
	}
	if d, err := time.Parse(time.DateOnly, a.StartDate); err == nil {
		m.ReleaseDate, m.Year = d, d.Year()
	}
	if a.Picture != "" {
		m.Artwork = []domain.Artwork{{Kind: domain.ArtworkPoster, URL: imageURL + a.Picture}}
	}
	if a.Rating.Votes > 0 {
		m.Ratings = []domain.Rating{{Site: domain.SiteAniDB, Score: a.Rating.Score * 10, Votes: a.Rating.Votes}}
	}
	return m
}

// roles are the creators kept, by the type AniDB names, and what they are credited as.
var roles = map[string]domain.CreditKind{
	"Direction": domain.CreditDirector, "Original Work": domain.CreditCreator, "Series Composition": domain.CreditWriter,
	"Music": domain.CreditComposer,
}

func (a anime) credits() []domain.Credit {
	var out []domain.Credit
	for _, c := range a.Characters {
		for _, s := range c.Seiyuu {
			credit := domain.Credit{Name: s.Name, IDs: map[domain.Provider]string{domain.ProviderAniDB: strconv.Itoa(s.ID)}, Kind: domain.CreditActor, Role: c.Name}
			if s.Picture != "" {
				credit.Photo = imageURL + s.Picture
			}
			out = append(out, credit)
		}
	}
	for _, c := range a.Creators {
		if kind, ok := roles[c.Type]; ok {
			out = append(out, domain.Credit{Name: c.Name, IDs: map[domain.Provider]string{domain.ProviderAniDB: strconv.Itoa(c.ID)}, Kind: kind, Role: c.Type})
		}
	}
	return out
}

func (e episode) metadata(loc domain.Locale) domain.Metadata {
	m := domain.Metadata{Title: name(e.Titles, loc), Overview: plain(e.Summary)}
	if d, err := time.Parse(time.DateOnly, e.AirDate); err == nil {
		m.ReleaseDate, m.Year = d, d.Year()
	}
	if e.Rating.Votes > 0 {
		m.Ratings = []domain.Rating{{Site: domain.SiteAniDB, Score: e.Rating.Score * 10, Votes: e.Rating.Votes}}
	}
	return m
}
