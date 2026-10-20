// Package radarr asks Radarr to fetch the films a library lacks that its profiles ask for, as
// Overseerr does: the film is added to Radarr, monitored and searched for, and Radarr writes it
// into the library's folder, where a scan finds it.
package radarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/arr"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// Client asks the Radarr at the address an admin sets.
type Client struct {
	settings provider.Settings
}

func New(settings provider.Settings) *Client {
	return &Client{settings: settings}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceRadarr, Name: "Radarr", Kinds: []domain.ItemKind{domain.ItemMovie},
		Settings: arr.Settings("Radarr", "http://radarr:7878"),
	}
}

type movie struct {
	Title               string     `json:"title"`
	TMDBID              int        `json:"tmdbId"`
	Year                int        `json:"year,omitempty"`
	QualityProfileID    int        `json:"qualityProfileId"`
	RootFolderPath      string     `json:"rootFolderPath"`
	Monitored           bool       `json:"monitored"`
	MinimumAvailability string     `json:"minimumAvailability"`
	AddOptions          addOptions `json:"addOptions"`
}

type addOptions struct {
	SearchForMovie bool `json:"searchForMovie"`
}

// Request adds a film to Radarr by its TMDB id, monitored and searched for at once, with the
// quality profile and root folder an admin named, or Radarr's first of each. A film Radarr has
// already is taken on as it is.
func (c *Client) Request(ctx context.Context, w domain.Wanted) error {
	if w.Kind != domain.ItemMovie {
		return fmt.Errorf("radarr fetches films, not a %s", w.Kind)
	}
	tmdb, err := strconv.Atoi(w.IDs[domain.ProviderTMDB])
	if err != nil {
		return errors.New("radarr knows a film by its TMDB id, and this one has none")
	}
	api, err := arr.New(ctx, "radarr", c.settings)
	if err != nil {
		return err
	}
	var had []json.RawMessage
	if err := api.Get(ctx, "/api/v3/movie", url.Values{"tmdbId": {strconv.Itoa(tmdb)}}, &had); err != nil || len(had) > 0 {
		return err
	}
	profile, folder, err := api.Place(ctx)
	if err != nil {
		return err
	}
	return api.Post(ctx, "/api/v3/movie", movie{
		Title: w.Title, TMDBID: tmdb, Year: w.Year, QualityProfileID: profile, RootFolderPath: folder,
		Monitored: true, MinimumAvailability: "released", AddOptions: addOptions{SearchForMovie: true},
	})
}
