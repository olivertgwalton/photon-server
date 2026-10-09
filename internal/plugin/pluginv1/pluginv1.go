// Package pluginv1 is version 1 of the protocol a metadata plugin speaks: the JSON it answers at
// GET {base}/manifest, and takes and answers at POST {base}/match, /describe, /search, /ratings
// and /person. Anything a plugin written against it could notice changing is a new version, in a
// package of its own; adding an optional field is not. docs/plugins.md is its reference.
package pluginv1

import "time"

// Version is the protocol version this package is.
const Version = 1

// Manifest is what a plugin is, what it does, and what an admin sets for it.
type Manifest struct {
	Protocol int `json:"protocol"`
	// ID is the plugin's slug: lower case letters, digits and hyphens, a letter or digit first.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kinds are "movie", "show" or both.
	Kinds []string `json:"kinds"`
	// Capabilities are "describe" (match and describe), "search", "rate" and "person".
	Capabilities []string  `json:"capabilities"`
	Settings     []Setting `json:"settings,omitempty"`
}

type Setting struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Secret is never answered back to an admin once set.
	Secret   bool `json:"secret,omitempty"`
	Required bool `json:"required,omitempty"`
}

// Capabilities are those a plugin speaking this protocol may answer.
var Capabilities = []string{"describe", "search", "rate", "person"}

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

// Problem is an error a plugin answers, as RFC 9457 shapes it, with a 4xx or 5xx status.
type Problem struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}
