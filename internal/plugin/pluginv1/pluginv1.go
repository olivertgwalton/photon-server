// Package pluginv1 is version 1 of the protocol a plugin speaks: the JSON it answers at
// GET {base}/manifest, and takes and answers at POST {base}/{capability}/v{version}/{call} for each
// capability its manifest names. Anything a plugin written against it could notice changing is a
// new version, in a package of its own; adding an optional field, a capability or a version of
// one is not. docs/plugins.md is its reference.
package pluginv1

import (
	"encoding/json"
	"time"
)

// Version is the protocol version this package is.
const Version = 1

// Manifest is what a plugin is, what it does, and what an admin sets for it.
type Manifest struct {
	Protocol int `json:"protocol"`
	// ID is the plugin's slug: lower case letters, digits and hyphens, a letter or digit first.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kinds are "movie", "show" or both.
	Kinds        []string     `json:"kinds"`
	Capabilities []Capability `json:"capabilities"`
	Settings     []Setting    `json:"settings,omitempty"`
}

// Capability is a family of calls a plugin answers, at a version of it. A plugin may name one at
// several versions, and the server calls the one it speaks; one it does not speak, by name or
// version, is passed over, so a plugin can answer a capability a newer server speaks.
type Capability struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Events are, of "events", the kinds of event the plugin is told of; one the server does not
	// raise is passed over.
	Events []string `json:"events,omitempty"`
	// Pages are, of "pages", the plugin's own pages the server's menus link to.
	Pages []Page `json:"pages,omitempty"`
}

// Page is a page the plugin serves, on its own origin: at URL, an http or https address or a path
// under the plugin's, for "admin"s alone or "everyone" signed in.
type Page struct {
	// ID is the page's slug, as Manifest's: lower case letters, digits and hyphens.
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Access string `json:"access"`
}

type Setting struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Secret is never answered back to an admin once set.
	Secret   bool `json:"secret,omitempty"`
	Required bool `json:"required,omitempty"`
}

// Speaks is the version of each capability this server speaks, and its calls: "describe" is
// match and describe, "search" is search, "rate" is ratings, "person" is person, "list" is list,
// "stream" is streams, "subtitles" is search and fetch, "events" is event, "segments" is markers, "pages" is no call: a page is visited.
var Speaks = map[string]int{"describe": 1, "search": 1, "rate": 1, "person": 1, "list": 1, "stream": 1, "subtitles": 1, "events": 1, "segments": 1, "pages": 1}

// Settings is in every request: what an admin set for the plugin, by key.
type Settings map[string]string

// Locale is what the server asks in, a library's or its own: the language its words are wanted
// in, an IETF tag such as en-GB, and the country whose certificates it wants, an ISO 3166-1
// alpha-2 code such as GB.
type Locale struct {
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
	// Artwork is "localized", for pictures in Language first, or "any", for the most liked.
	Artwork string `json:"artwork,omitempty"`
}

type MatchRequest struct {
	Settings Settings `json:"settings"`
	Locale
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
	// IDs are the ids the title carries, by provider: "imdb", "tmdb", "tvdb", and this plugin's
	// own, "plugin:<id>", where it gave one before.
	IDs map[string]string `json:"ids"`
}

// MatchResponse is the title's id on the plugin; empty is no confident match.
type MatchResponse struct {
	ID string `json:"id"`
}

type DescribeRequest struct {
	Settings Settings `json:"settings"`
	Locale
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Seasons are a show's seasons to describe, in the order its files are numbered in: "aired",
	// "dvd" or "absolute".
	Seasons []int  `json:"seasons,omitempty"`
	Order   string `json:"order,omitempty"`
	// SeasonScope is "every" for every season the plugin has, else "numbered", for Seasons.
	SeasonScope string `json:"season_scope,omitempty"`
}

type DescribeResponse struct {
	Metadata
	Seasons []Season `json:"seasons,omitempty"`
}

// Metadata is what the plugin knows of a title, a season or an episode. A field left out says
// nothing.
type Metadata struct {
	Title         string `json:"title,omitempty"`
	SortTitle     string `json:"sort_title,omitempty"`
	OriginalTitle string `json:"original_title,omitempty"`
	Overview      string `json:"overview,omitempty"`
	Tagline       string `json:"tagline,omitempty"`
	Certificate   string `json:"certificate,omitempty"`
	// ReleaseDate is a date, as 2006-01-02.
	ReleaseDate string            `json:"release_date,omitempty"`
	Year        int               `json:"year,omitempty"`
	Genres      []string          `json:"genres,omitempty"`
	Studios     []string          `json:"studios,omitempty"`
	IDs         map[string]string `json:"ids,omitempty"`
	Artwork     []Artwork         `json:"artwork,omitempty"`
	Videos      []Video           `json:"videos,omitempty"`
	Credits     []Credit          `json:"credits,omitempty"`
}

type Season struct {
	Number int `json:"number"`
	Metadata
	Episodes []Episode `json:"episodes,omitempty"`
}

type Episode struct {
	Number int `json:"number"`
	Metadata
}

// Artwork is a picture the server fetches from URL, http or https.
type Artwork struct {
	// Kind is "poster", "backdrop", "logo", "thumb" or "banner".
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Language string `json:"language,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// Video is a video hosted elsewhere: a trailer on YouTube is site "YouTube" and its video key.
type Video struct {
	// Kind is "trailer", "teaser", "featurette", "behind_the_scenes", "deleted_scene",
	// "interview", "scene", "short", "clip", "blooper", "theme_video" or "other".
	Kind      string    `json:"kind"`
	Site      string    `json:"site"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Language  string    `json:"language,omitempty"`
	Published time.Time `json:"published,omitzero"`
}

type Credit struct {
	Name string `json:"name"`
	// IDs are the person's, by provider; one with none is not kept. Two credits sharing an id, from
	// any source, are one person.
	IDs   map[string]string `json:"ids"`
	Photo string            `json:"photo,omitempty"`
	// Kind is "actor", "guest_star", "director", "writer", "producer", "composer" or "creator".
	Kind string `json:"kind"`
	// Role is the character an actor plays or the job a crew member did.
	Role string `json:"role,omitempty"`
}

type SearchRequest struct {
	Settings Settings `json:"settings"`
	Locale
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}

type SearchResponse struct {
	Results []Candidate `json:"results"`
}

type Candidate struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
	Overview      string `json:"overview,omitempty"`
	Poster        string `json:"poster,omitempty"`
}

type RatingsRequest struct {
	Settings Settings          `json:"settings"`
	Kind     string            `json:"kind"`
	IDs      map[string]string `json:"ids"`
}

type RatingsResponse struct {
	Ratings []Rating `json:"ratings"`
}

type Rating struct {
	// Site is "imdb", "tmdb", "rotten_tomatoes" or "rotten_tomatoes_audience"; any other is dropped.
	Site string `json:"site"`
	// Score is out of 100, whatever scale the site uses.
	Score float64 `json:"score"`
	Votes int     `json:"votes,omitempty"`
}

type PersonRequest struct {
	Settings Settings `json:"settings"`
	Locale
	IDs map[string]string `json:"ids"`
}

// PersonResponse is what the plugin knows of someone; a plugin that does not know them answers
// 404.
type PersonResponse struct {
	Name       string `json:"name"`
	Biography  string `json:"biography,omitempty"`
	Born       string `json:"born,omitempty"`
	Died       string `json:"died,omitempty"`
	Birthplace string `json:"birthplace,omitempty"`
	Photo      string `json:"photo,omitempty"`
}

type ListRequest struct {
	Settings Settings `json:"settings"`
	// ID is the list's, as the plugin names its lists and an admin gives it.
	ID string `json:"id"`
}

// ListResponse is a list's titles in its order; a list the plugin does not have is answered 404.
type ListResponse struct {
	Titles []Listed `json:"titles"`
}

// Listed is a film or a show a list holds. One with no ids is left out.
type Listed struct {
	Kind  string            `json:"kind"`
	IDs   map[string]string `json:"ids"`
	Title string            `json:"title,omitempty"`
	Year  int               `json:"year,omitempty"`
}

// StreamsRequest asks for the streams of a film by its ids, or of an episode by its show's ids
// and its numbers.
type StreamsRequest struct {
	Settings Settings          `json:"settings"`
	Kind     string            `json:"kind"`
	IDs      map[string]string `json:"ids"`
	Season   int               `json:"season,omitempty"`
	Episode  int               `json:"episode,omitempty"`
}

// StreamsResponse is the streams the plugin offers, best first; none is no stream.
type StreamsResponse struct {
	Streams []Stream `json:"streams"`
}

// Stream is a copy the server plays from URL, http or https, which may be the plugin's own host.
type Stream struct {
	// Key is which bytes the stream is, the same each time it is offered though its URL changes:
	// a file's name and size, or a torrent's hash and file. One with none is left out.
	Key      string `json:"key"`
	URL      string `json:"url"`
	Name     string `json:"name,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// SubtitlesRequest asks for the subtitles of a film by its ids, or of an episode by its show's ids
// and its numbers, in a language.
type SubtitlesRequest struct {
	Settings Settings          `json:"settings"`
	Kind     string            `json:"kind"`
	IDs      map[string]string `json:"ids"`
	Season   int               `json:"season,omitempty"`
	Episode  int               `json:"episode,omitempty"`
	// Hash is the file's OpenSubtitles hash, where the copy is one file the server can read.
	Hash string `json:"hash,omitempty"`
	// Language is an IETF tag, such as en or pt-BR.
	Language string `json:"language"`
}

type SubtitlesResponse struct {
	Subtitles []Subtitle `json:"subtitles"`
}

// Subtitle is one the plugin has: its id there, fetched by it, and what it is.
type Subtitle struct {
	ID              string `json:"id"`
	Language        string `json:"language"`
	Release         string `json:"release,omitempty"`
	HearingImpaired bool   `json:"hearing_impaired,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	// ForRelease is a subtitle made for the very file the hash is of.
	ForRelease bool `json:"for_release,omitempty"`
	Downloads  int  `json:"downloads,omitempty"`
}

type FetchRequest struct {
	Settings Settings `json:"settings"`
	ID       string   `json:"id"`
}

// FetchResponse is a subtitle as SubRip.
type FetchResponse struct {
	SubRip string `json:"subrip"`
}

// EventRequest is an event the plugin hears, as a webhook is sent it, with its settings as they
// are when it is sent.
type EventRequest struct {
	Settings Settings        `json:"settings"`
	Event    json.RawMessage `json:"event"`
}

// MarkersRequest asks for the intro and credits of a film by its ids, or of an episode by its
// show's ids and its numbers, in a copy of a length: a release's timing is of that release.
type MarkersRequest struct {
	Settings   Settings          `json:"settings"`
	Kind       string            `json:"kind"`
	IDs        map[string]string `json:"ids"`
	Season     int               `json:"season,omitempty"`
	Episode    int               `json:"episode,omitempty"`
	DurationMS int64             `json:"duration_ms"`
}

// MarkersResponse is the stretches the plugin has timed; none is none known for that copy.
type MarkersResponse struct {
	Markers []Marker `json:"markers"`
}

type Marker struct {
	// Kind is "intro", "credits", "recap" or "preview"; any other is dropped.
	Kind    string `json:"kind"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

// Problem is an error a plugin answers, as RFC 9457 shapes it, with a 4xx or 5xx status.
type Problem struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}
